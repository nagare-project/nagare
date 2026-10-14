import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { ApiAuthError, ApiError } from '../lib/api'
import { installSource, previewSourceInstall } from '../lib/endpoints'
import type { SourcePreview, SourcePreviewMember, SourceSummary } from '../lib/endpoints'
import { errorText, formatBytes, sourceKindLabel } from '../lib/format'
import { label, mono } from '../theme'
import '../components/library/library.css'
import './settings.css'
import './source-install.css'

type PreviewState =
  | { phase: 'loading' }
  | { phase: 'error'; message: string; unavailable: boolean }
  | { phase: 'ready'; preview: SourcePreview }

type InstallState =
  | { phase: 'idle' }
  | { phase: 'installing' }
  | { phase: 'error'; message: string }
  | { phase: 'done'; name: string }

/**
 * `/sources/install?url=&sha256=` 从地址安装一个来源文件或来源包。
 *
 * 来源包把一组来源放在一个文件里，确认一次、插件只重启一次；之后整体更新、整体卸载。
 *
 * 外部页面只能把用户带到这里：预览是只读的（下载 + 交给插件校验，不写盘），
 * 真正安装必须由用户在本页点「安装」。nagare 不内置、不推荐任何来源地址，
 * 这一页也不预填任何地址。
 */
export function SourceInstallPage() {
  const { url, sha256 } = useSearch({ from: '/sources/install' })
  const [unauthorized, setUnauthorized] = useState(false)

  if (unauthorized) return <UnauthorizedNotice />

  return <main className="lib-shell">
    <header className="page-head">
      <h1 className="page-title">安装来源</h1>
    </header>
    {url === undefined
      ? <AddressForm />
      : <InstallReview key={`${url}\n${sha256 ?? ''}`} url={url} sha256={sha256 ?? ''}
        onUnauthorized={() => setUnauthorized(true)} />}
  </main>
}

function AddressForm() {
  const navigate = useNavigate({ from: '/sources/install' })
  const [url, setUrl] = useState('')
  const [sha256, setSha256] = useState('')

  function handleSubmit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    const nextUrl = url.trim()
    const nextSha = sha256.trim()
    if (nextUrl === '') return
    void navigate({ search: { url: nextUrl, sha256: nextSha === '' ? undefined : nextSha } })
  }

  return <section className="panel settings-card" aria-labelledby="source-install-form-heading">
    <h2 id="source-install-form-heading" className="panel-heading">来源文件地址</h2>
    <p className="page-notice-copy">
      填入一个 Nagare Source 来源文件（JSON 或 YAML）或来源包的 HTTPS 地址。安装前会先下载并交给本机插件逐个校验，你确认后才会写入。
    </p>
    <form className="source-install-form" onSubmit={handleSubmit}>
      <div className="field">
        <label htmlFor="source-install-url" style={label}>地址</label>
        <input id="source-install-url" className="input" type="url" value={url}
          onChange={event => setUrl(event.target.value)} placeholder="https://…/source.json"
          spellCheck={false} autoComplete="off" required />
      </div>
      <div className="field">
        <label htmlFor="source-install-sha" style={label}>SHA-256（可选）</label>
        <input id="source-install-sha" className="input" value={sha256}
          onChange={event => setSha256(event.target.value)} placeholder="发布方给出的摘要，填了就会比对"
          spellCheck={false} autoComplete="off" />
      </div>
      <div className="form-actions">
        <button type="submit" className="btn btn--sm btn--primary">预览</button>
        <Link to="/settings" hash="sources" className="btn btn--sm">返回设置</Link>
      </div>
    </form>
  </section>
}

function InstallReview({ url, sha256, onUnauthorized }: { url: string; sha256: string; onUnauthorized: () => void }) {
  const [preview, setPreview] = useState<PreviewState>({ phase: 'loading' })
  const [install, setInstall] = useState<InstallState>({ phase: 'idle' })
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    setPreview({ phase: 'loading' })
    previewSourceInstall(url, sha256, controller.signal)
      .then(data => setPreview({ phase: 'ready', preview: data }))
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        if (err instanceof ApiAuthError) {
          onUnauthorized()
          return
        }
        setPreview({
          phase: 'error',
          message: errorText(err, '读取来源文件失败'),
          unavailable: err instanceof ApiError && err.status === 503,
        })
      })
    return () => controller.abort()
  }, [url, sha256, attempt])

  async function handleInstall(data: SourcePreview): Promise<void> {
    setInstall({ phase: 'installing' })
    try {
      await installSource(data.url, data.sha256)
      setInstall({ phase: 'done', name: data.package.name })
    } catch (err) {
      if (err instanceof ApiAuthError) {
        onUnauthorized()
        return
      }
      setInstall({ phase: 'error', message: errorText(err, '安装来源失败') })
    }
  }

  if (preview.phase === 'loading') {
    return <section className="panel settings-card">
      <p className="result result--dim" role="status">正在下载并校验来源…</p>
      <p className="source-install-url" style={mono}>{url}</p>
    </section>
  }

  if (preview.phase === 'error') {
    return <section className="panel settings-card">
      <p className="result result--err" role="alert">{preview.message}</p>
      <p className="source-install-url" style={mono}>{url}</p>
      {preview.unavailable && <p className="page-notice-copy">
        需要先在设置里启用 Nagare Source，并确认插件版本支持安装来源。
      </p>}
      <div className="form-actions">
        <button type="button" className="btn btn--sm" onClick={() => setAttempt(n => n + 1)}>重试</button>
        <Link to="/settings" hash="sources" className="btn btn--sm">打开来源设置</Link>
        <Link to="/sources/install" className="link">换一个地址</Link>
      </div>
    </section>
  }

  const data = preview.preview
  if (install.phase === 'done') {
    return <section className="panel settings-card">
      <p className="result result--ok" role="status">已安装「{install.name}」，来源插件已重新加载。</p>
      <div className="form-actions">
        <Link to="/settings" hash="sources" className="btn btn--sm btn--primary">查看来源设置</Link>
      </div>
    </section>
  }

  const pkg = data.package
  const disabled = data.sources.filter(member => !member.source.enabled)
  const blocked = disabled.length > 0 || data.conflicts.length > 0
  const busy = install.phase === 'installing'
  return <section className="panel settings-card source-install-review" aria-labelledby="source-install-heading">
    <h2 id="source-install-heading" className="panel-heading">{pkg.name}</h2>
    <p className="source-install-id" style={mono}>
      {pkg.pack ? `来源包 · ${data.sources.length} 个来源 · v${pkg.version}` : `${pkg.id} · v${pkg.version}`}
    </p>
    {pkg.description && <p className="page-notice-copy">{pkg.description}</p>}

    <p className="result result--warn" role="note">
      来源文件由第三方编写，nagare 不审核也不推荐任何来源。安装后插件会按文件内容访问下面列出的域名。
    </p>
    <ReplaceNotice preview={data} />
    {data.conflicts.length > 0 && <div className="result result--err" role="alert">
      <p>下面的来源已由其他安装项提供，请先在来源设置里卸载对应项再安装：</p>
      <ul className="source-install-conflicts">
        {data.conflicts.map(conflict => <li key={conflict.sourceId}>
          <span style={mono}>{conflict.sourceId}</span>（「{conflict.packageName}」）
        </li>)}
      </ul>
    </div>}
    {data.sources.some(member => member.source.kind === 'web') && <p className="result result--dim" role="note">
      在线播放来源需要本机安装 Chrome 或 Chromium 才能解析播放地址。
    </p>}

    {pkg.pack
      ? <ul className="source-install-members" aria-label="包含的来源">
        {data.sources.map(member => <PackMember key={member.source.id} member={member} />)}
      </ul>
      : <SourceDetails source={data.sources[0]!.source} />}

    <dl className="kv-list">
      <dt>文件地址</dt><dd className="source-install-url" style={mono}>{data.url}</dd>
      <dt>SHA-256</dt><dd className="source-install-url" style={mono}>{data.sha256}</dd>
      <dt>大小</dt><dd>{formatBytes(data.bytes)} · {data.format.toUpperCase()}</dd>
    </dl>

    <details className="source-install-content">
      <summary>查看文件原文</summary>
      <pre style={mono}>{data.content}</pre>
    </details>

    {disabled.length > 0 && <p className="result result--err" role="alert">
      {pkg.pack
        ? `来源 ${disabled.map(member => member.source.id).join('、')} 在文件里标记为停用，不能安装。`
        : '这个来源在文件里标记为停用，不能安装。'}
    </p>}
    {install.phase === 'error' && <p className="result result--err" role="alert">{install.message}</p>}
    <div className="form-actions">
      <button type="button" className="btn btn--sm btn--primary" disabled={busy || blocked}
        onClick={() => void handleInstall(data)}>
        {busy ? '正在安装…' : data.installed ? '更新' : '安装'}
      </button>
      <Link to="/settings" hash="sources" className="btn btn--sm">取消</Link>
    </div>
  </section>
}

function SourceDetails({ source }: { source: SourceSummary }) {
  return <dl className="kv-list">
    <dt>类型</dt><dd>{sourceKindLabel(source.kind)}（第 {source.tier} 层）</dd>
    <dt>可访问域名</dt><dd><Hosts hosts={source.allowedHosts} /></dd>
    <dt>许可</dt><dd>{source.license}</dd>
    <dt>生态</dt><dd>{source.ecosystem}</dd>
    <dt>上游</dt><dd style={mono}>{source.upstream}</dd>
    {source.homepage && <><dt>主页</dt><dd style={mono}>{source.homepage}</dd></>}
  </dl>
}

function PackMember({ member }: { member: SourcePreviewMember }) {
  const { source, existing } = member
  return <li>
    <div className="source-install-member-head">
      <strong>{source.name}</strong>
      <span className="source-install-kind">{sourceKindLabel(source.kind)} · 第 {source.tier} 层</span>
    </div>
    <small style={mono}>{source.id} · v{source.version}</small>
    {source.description && <small>{source.description}</small>}
    <div className="source-install-member-hosts"><span>可访问</span><Hosts hosts={source.allowedHosts} /></div>
    {existing && <small>插件当前已有同 id 的来源「{existing.name}」（v{existing.version}），安装后以这个文件为准。</small>}
  </li>
}

function Hosts({ hosts }: { hosts: string[] }) {
  if (hosts.length === 0) return <>未声明</>
  return <ul className="source-install-hosts">
    {hosts.map(host => <li key={host} style={mono}>{host}</li>)}
  </ul>
}

function ReplaceNotice({ preview }: { preview: SourcePreview }) {
  const installed = preview.installed
  if (installed) {
    if (installed.sha256 === preview.sha256) {
      return <p className="result result--dim" role="status">
        已经安装过这个文件（v{installed.version}），再次安装不会有变化。
      </p>
    }
    const incoming = new Set(preview.sources.map(member => member.source.id))
    const dropped = installed.sources.filter(source => !incoming.has(source.id))
    return <p className="result result--dim" role="status">
      已安装 v{installed.version}（来自 {hostOf(installed.url)}），确认后将替换为这个文件。
      {dropped.length > 0 && ` 新版本不再包含 ${dropped.map(source => source.name).join('、')}，会一并移除。`}
    </p>
  }
  const existing = preview.package.pack ? undefined : preview.sources[0]?.existing
  if (existing) {
    return <p className="result result--dim" role="status">
      插件当前已有同 id 的来源「{existing.name}」（v{existing.version}），安装后以这个文件为准。
    </p>
  }
  return null
}

function hostOf(raw: string): string {
  try {
    return new URL(raw).host
  } catch {
    return raw
  }
}
