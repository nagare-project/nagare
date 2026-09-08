import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import type { UseSourcePluginResult } from '../../hooks/useSourcePlugin'
import { errorText } from '../../lib/format'
import { label, mono } from '../../theme'
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
    setExecutable(view.config.executable)
    setRoot(view.config.root)
  }, [view?.config.enabled, view?.config.executable, view?.config.root])

  async function handleSave(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault()
    setBusy(true)
    setResult(null)
    setError(null)
    try {
      const next = await plugin.save({ enabled, executable: executable.trim(), root: root.trim() })
      setResult(next.config.enabled ? '已启用并连接来源插件' : '已停用来源插件')
    } catch (err) {
      setError(errorText(err, '保存来源插件配置失败'))
    } finally {
      setBusy(false)
    }
  }

  return <section className="panel settings-card" aria-labelledby="source-plugin-heading">
    <h2 id="source-plugin-heading" className="panel-heading">本地来源插件</h2>
    <p className="page-notice-copy">
      nagare 不内置在线来源。只有在你明确启用后，才会启动所选的本地插件；
      作品标题、集号和公开元数据会交给它找源。
    </p>

    {plugin.state.phase === 'loading' && <p className="result result--dim" role="status">正在读取插件设置…</p>}
    {plugin.state.phase === 'error' && <div>
      <p className="result result--err" role="alert">{plugin.state.message}</p>
      <button type="button" className="btn btn--sm" onClick={() => void plugin.reload()}>重试</button>
    </div>}
    {view !== null && <>
      <PluginStatus view={view} />
      <form className="source-plugin-form" onSubmit={event => void handleSave(event)}>
        <label className="source-plugin-toggle">
          <button type="button" role="switch" className="switch" aria-checked={enabled}
            aria-label="启用本地来源插件" onClick={() => setEnabled(!enabled)} disabled={busy}>
            <span className="switch-knob" aria-hidden="true" />
          </button>
          <span><strong>启用本地来源插件</strong><small>关闭时会立即终止插件进程。</small></span>
        </label>
        <div className="field">
          <label htmlFor="source-plugin-executable" style={label}>插件可执行文件</label>
          <input id="source-plugin-executable" className="input" value={executable}
            onChange={event => setExecutable(event.target.value)} placeholder="/absolute/path/to/nagare-source"
            spellCheck={false} autoComplete="off" disabled={busy} required={enabled} />
        </div>
        <div className="field">
          <label htmlFor="source-plugin-root" style={label}>来源仓库目录</label>
          <input id="source-plugin-root" className="input" value={root}
            onChange={event => setRoot(event.target.value)} placeholder="/absolute/path/to/source-repository"
            spellCheck={false} autoComplete="off" disabled={busy} required={enabled} />
          <p className="source-plugin-note">两个路径都必须是本机绝对路径。插件只能通过回环地址与 nagare 通信。</p>
        </div>
        <div className="form-actions">
          <button type="submit" className="btn btn--sm btn--primary" disabled={busy}>{busy ? '正在验证…' : '保存并应用'}</button>
          {result !== null && <span className="result result--ok" role="status">{result}</span>}
        </div>
        {error !== null && <p className="result result--err" role="alert">{error}</p>}
      </form>
      {view.sourcesError && <p className="result result--err" role="alert">{view.sourcesError}</p>}
      <div className="source-plugin-sources">
        <h3>已公布来源 · {view.sources.length}</h3>
        {view.sources.length === 0 ? <p className="result result--dim">插件尚未公布可用来源。</p> : <ul>
          {view.sources.map(source => <li key={source.id}>
            <span><strong>{source.name}</strong><small style={mono}>{source.id} · v{source.version}</small></span>
            <span className={source.enabled && source.status === 'ready' ? 'result result--ok' : 'result result--dim'}>
              {source.enabled ? source.status : '已停用'}
            </span>
          </li>)}
        </ul>}
      </div>
    </>}
  </section>
}

function PluginStatus({ view }: { view: Extract<UseSourcePluginResult['state'], { phase: 'ready' }>['data'] }) {
  const labels: Record<string, string> = { disabled: '未启用', starting: '正在启动', ready: '已就绪', stopped: '已停止', failed: '启动失败' }
  return <dl className="kv-list">
    <dt>状态</dt><dd>{labels[view.status.phase] ?? view.status.phase}</dd>
    {view.status.manifest && <><dt>插件</dt><dd>{view.status.manifest.name} · v{view.status.manifest.version}</dd></>}
    {view.status.error && <><dt>错误</dt><dd className="result result--err">{view.status.error}</dd></>}
  </dl>
}
