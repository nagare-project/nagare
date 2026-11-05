import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import type { UseSourcePluginResult } from '../../hooks/useSourcePlugin'
import { inFlight } from '../../lib/busy'
import type { PluginSource } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { mono } from '../../theme'
import '../../styles/panels.css'
import './source-plugin.css'

export function SourcePluginCard({ plugin }: { plugin: UseSourcePluginResult }) {
  const [enabled, setEnabled] = useState(false)
  const [executable, setExecutable] = useState('')
  const [root, setRoot] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const view = plugin.state.phase === 'ready' ? plugin.state.data : null
  useEffect(() => {
    if (view === null) return
    setEnabled(view.config.enabled)
    setExecutable(view.config.executable ?? '')
    setRoot(view.config.root ?? '')
  }, [view?.config.enabled, view?.config.executable, view?.config.root])

  async function handleSave(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault()
    if (busy) return
    await apply({ enabled, executable: executable.trim(), root: root.trim() })
  }

  async function useBundled(): Promise<void> {
    await apply({ enabled: true, executable: '', root: '', useBundled: true })
  }

  async function apply(config: Parameters<typeof plugin.save>[0]): Promise<void> {
    setBusy(true)
    setResult(null)
    setError(null)
    try {
      const next = await plugin.save(config)
      setResult(next.config.enabled ? '已启用并连接来源插件' : '已停用来源插件')
    } catch (err) {
      setError(errorText(err, '保存来源插件配置失败'))
    } finally {
      setBusy(false)
    }
  }

  return <section className="info-panel" aria-labelledby="source-plugin-heading">
    <h3 id="source-plugin-heading" className="info-panel-title">Nagare Source</h3>
    <p className="info-panel-text">
      连接本机安装的 Nagare Source 后，作品页可以统一查找在线与 BT 候选并自动换源。只有在你明确启用后，nagare 才会启动该进程并交给它作品标题、集号和公开元数据。{' '}<a className="link" href="https://github.com/nagare-project/Nagare_Source" target="_blank" rel="noreferrer noopener">安装说明 ↗</a>
    </p>
    {view?.bundled && <p className="info-panel-note source-plugin-bundled" role="status">
      {view.bundled.active
        ? '正在使用安装包内置的 Nagare Source（只含 BT 来源，在线来源需另行提供仓库地址）。'
        : '这个安装包内置了 Nagare Source（只含 BT 来源）。'}
      {!view.bundled.active && <> <button type="button" className="link" {...inFlight(busy, () => void useBundled())}>使用内置插件</button></>}
    </p>}

    {plugin.state.phase === 'loading' && <p className="result result--dim" role="status">正在读取插件设置…</p>}
    {plugin.state.phase === 'error' && <div className="form-actions">
      <p className="result result--err" role="alert">{plugin.state.message}</p>
      <button type="button" className="btn btn--sm" onClick={() => void plugin.reload()}>重试</button>
    </div>}
    {view !== null && <>
      <PluginStatus view={view} />
      <form className="source-plugin-form" onSubmit={event => void handleSave(event)}>
        <label className="toggle-row">
          <span id="source-plugin-enabled-label" className="toggle-row-label">启用 Nagare Source</span>
          <span id="source-plugin-enabled-note" className="toggle-row-note">关闭时会立即终止来源进程。</span>
          <button type="button" role="switch" className="switch" aria-checked={enabled}
            aria-labelledby="source-plugin-enabled-label" aria-describedby="source-plugin-enabled-note" {...inFlight(busy, () => setEnabled(!enabled))}>
            <span className="switch-knob" aria-hidden="true" />
          </button>
        </label>
        <div className="field">
          <label htmlFor="source-plugin-executable" className="field-label">插件可执行文件</label>
          <input id="source-plugin-executable" className="input" value={executable} style={mono}
            onChange={event => setExecutable(event.target.value)} placeholder="/absolute/path/to/nagare-source"
            spellCheck={false} autoComplete="off" readOnly={busy} required={enabled} />
        </div>
        <div className="field">
          <label htmlFor="source-plugin-root" className="field-label">Nagare Source 仓库目录</label>
          <input id="source-plugin-root" className="input" value={root} style={mono}
            onChange={event => setRoot(event.target.value)} placeholder="/absolute/path/to/source-repository"
            spellCheck={false} autoComplete="off" readOnly={busy} required={enabled} aria-describedby="source-plugin-root-note" />
          <p id="source-plugin-root-note" className="field-note">两个路径都必须是本机绝对路径。插件只能通过回环地址与 nagare 通信。</p>
        </div>
        <div className="form-actions">
          <button type="submit" className="btn btn--primary" {...inFlight(busy)}>{busy ? '正在验证…' : '保存并应用'}</button>
          {/* 常驻的空 live region：与结果同时插进来的话，读屏多半念不出来 */}
          <span className={result === null ? 'result' : 'result result--ok'} role="status">{result}</span>
        </div>
        {error !== null && <p className="result result--err" role="alert">{error}</p>}
      </form>
      {view.sourcesError && <p className="result result--err" role="alert">{view.sourcesError}</p>}
      <div className="source-plugin-sources">
        <h4 className="info-panel-subtitle">已公布来源 · {view.sources.length}</h4>
        {view.sources.length === 0 ? <p className="info-panel-note">插件尚未公布可用来源。</p> : <ul>
          {view.sources.map(source => <li key={source.id}>
            <span><strong>{source.name}</strong><small style={mono}>{source.id} · v{source.version}</small></span>
            <span className={`status-text ${sourceStatusClass(source)}`}>
              {sourceStatusLabel(source)}
            </span>
          </li>)}
        </ul>}
      </div>
    </>}
  </section>
}

function sourceStatusLabel(source: PluginSource): string {
  if (!source.enabled || source.status === 'disabled') return '已停用'
  const labels: Record<PluginSource['status'], string> = {
    healthy: '正常',
    degraded: '可用（待验证）',
    unavailable: '不可用',
    interactive_required: '需要人工验证',
    disabled: '已停用',
  }
  return labels[source.status]
}

function sourceStatusClass(source: PluginSource): string {
  if (!source.enabled || source.status === 'disabled') return 'status-text--idle'
  if (source.status === 'healthy') return 'status-text--ok'
  if (source.status === 'unavailable') return 'status-text--error'
  return 'status-text--warn'
}

function PluginStatus({ view }: { view: Extract<UseSourcePluginResult['state'], { phase: 'ready' }>['data'] }) {
  const labels: Record<string, string> = { disabled: '未启用', starting: '正在启动', ready: '已就绪', stopped: '已停止', failed: '启动失败' }
  const tones: Record<string, string> = { ready: 'status-text--ok', starting: 'status-text--warn', failed: 'status-text--error' }
  const tone = tones[view.status.phase] ?? 'status-text--idle'
  return <dl className="kv-list">
    <dt>状态</dt><dd className={`status-text ${tone}`}>{labels[view.status.phase] ?? view.status.phase}</dd>
    {view.status.manifest && <><dt>插件</dt><dd>{view.status.manifest.name} · v{view.status.manifest.version}</dd></>}
    {view.status.error && <><dt>错误</dt><dd className="status-text status-text--error">{view.status.error}</dd></>}
  </dl>
}
