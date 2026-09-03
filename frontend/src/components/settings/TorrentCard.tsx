import { useState } from 'react'
import type { FormEvent } from 'react'
import { clearTorrentCache, updateTorrentConfig } from '../../lib/endpoints'
import type { TorrentSettings } from '../../lib/endpoints'
import { errorText, formatBytes } from '../../lib/format'
import { label, mono } from '../../theme'
import './cards.css'

/** 端口校验失败的文案（0 = 交给系统随机分配） */
export const PORT_HINT = '监听端口必须是 0–65535 的整数（0 表示由系统随机分配）'

/** 引擎起不来时的提示：降级运行可以，但不能只是按钮点了没反应 */
export const ENGINE_DOWN_HINT =
  '磁力引擎启动失败，边下边播不可用。请查看日志（本页「关于」里有日志路径），确认缓存目录可写、监听端口没被占用。'

type Action = 'save' | 'clear'

/** 动作结果行状态机 */
type ActionState =
  | { phase: 'idle' }
  | { phase: 'busy'; action: Action }
  | { phase: 'ok'; text: string }
  | { phase: 'error'; message: string }

export interface TorrentCardProps {
  torrent: TorrentSettings
  /** 保存 / 清缓存成功后刷新设置，让别处（搜索页的播放按钮）跟着变 */
  onReload: () => Promise<void>
}

/**
 * 设置页「磁力」卡：持续做种 · 自动端口映射 · 监听端口 · tracker 列表 · 缓存占用。
 *
 * 四项配置一起保存（一次 POST /api/torrent/config）：端口类改动后端会回
 * restartRequired，界面据此提示重启。表单脏了会显式提示「有未保存的改动」，
 * 免得开关拨完以为已经生效。
 */
export function TorrentCard({ torrent, onReload }: TorrentCardProps) {
  // saved = 后端已生效的值，用来判断表单是否脏
  const [saved, setSaved] = useState<TorrentSettings>(torrent)
  const [seeding, setSeeding] = useState(torrent.seeding)
  const [portForwarding, setPortForwarding] = useState(torrent.portForwarding)
  const [listenPort, setListenPort] = useState(String(torrent.listenPort))
  const [trackersText, setTrackersText] = useState(torrent.trackers.join('\n'))
  const [restartRequired, setRestartRequired] = useState(false)
  const [state, setState] = useState<ActionState>({ phase: 'idle' })

  const busy = state.phase === 'busy'
  const disabled = busy || !torrent.enabled
  const trackers = parseTrackers(trackersText)
  const dirty =
    seeding !== saved.seeding ||
    portForwarding !== saved.portForwarding ||
    listenPort.trim() !== String(saved.listenPort) ||
    trackers.join('\n') !== saved.trackers.join('\n')

  function handleSave(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    const port = parsePort(listenPort)
    if (port === null) {
      setState({ phase: 'error', message: PORT_HINT })
      return
    }
    setState({ phase: 'busy', action: 'save' })
    void (async () => {
      try {
        const next = await updateTorrentConfig({ seeding, portForwarding, listenPort: port, trackers })
        // 以后端返回值为准回填：后端可能规范化端口 / 去重 tracker
        setSaved(next)
        setSeeding(next.seeding)
        setPortForwarding(next.portForwarding)
        setListenPort(String(next.listenPort))
        setTrackersText(next.trackers.join('\n'))
        setRestartRequired(next.restartRequired)
        setState({ phase: 'ok', text: '已保存' })
        await onReload()
      } catch (err) {
        console.error('保存磁力设置失败', err)
        setState({ phase: 'error', message: errorText(err, '保存磁力设置失败') })
      }
    })()
  }

  function handleClearCache(): void {
    setState({ phase: 'busy', action: 'clear' })
    void (async () => {
      try {
        const result = await clearTorrentCache()
        setSaved((current) => ({ ...current, cacheBytes: result.cacheBytes }))
        setState({ phase: 'ok', text: `已清空缓存 · 当前占用 ${formatBytes(result.cacheBytes)}` })
        await onReload()
      } catch (err) {
        console.error('清空磁力缓存失败', err)
        setState({ phase: 'error', message: errorText(err, '清空磁力缓存失败') })
      }
    })()
  }

  const view = actionView(state)

  return (
    <section className="panel settings-card" aria-labelledby="torrent-heading">
      <h2 id="torrent-heading" className="panel-heading">
        磁力边下边播
      </h2>

      {!torrent.enabled && (
        <p className="alert-warn" role="alert">
          {ENGINE_DOWN_HINT}
        </p>
      )}

      <form className="torrent-form" onSubmit={handleSave}>
        <label className="torrent-toggle">
          <button
            type="button"
            role="switch"
            className="switch"
            aria-checked={seeding}
            aria-label="持续做种"
            onClick={() => setSeeding(!seeding)}
            disabled={disabled}
          >
            <span className="switch-knob" aria-hidden="true" />
          </button>
          <span className="torrent-toggle-body">
            <span className="torrent-toggle-text">持续做种</span>
            <span className="torrent-toggle-note">
              播放期间的分片交换是 BT 协议必需的，这个开关不影响它；它只决定
              <b>停止播放之后是否继续上传</b>。默认关闭。
            </span>
          </span>
        </label>

        <label className="torrent-toggle">
          <button
            type="button"
            role="switch"
            className="switch"
            aria-checked={portForwarding}
            aria-label="自动端口映射"
            onClick={() => setPortForwarding(!portForwarding)}
            disabled={disabled}
          >
            <span className="switch-knob" aria-hidden="true" />
          </button>
          <span className="torrent-toggle-body">
            <span className="torrent-toggle-text">自动端口映射（UPnP / NAT-PMP）</span>
            <span className="torrent-toggle-note">
              让路由器把监听端口映射到外网，能连上更多分享者。路由器不支持时不影响使用。
            </span>
          </span>
        </label>

        <div className="field">
          <label htmlFor="torrent-listen-port" style={label}>
            监听端口
          </label>
          <input
            id="torrent-listen-port"
            className="input torrent-port"
            type="text"
            inputMode="numeric"
            value={listenPort}
            onChange={(event) => setListenPort(event.target.value)}
            spellCheck={false}
            autoComplete="off"
            disabled={disabled}
            aria-describedby="torrent-port-note"
          />
          <p id="torrent-port-note" className="torrent-note">
            0–65535；填 0 由系统随机分配。改动后需要重启 nagare 才生效。
          </p>
        </div>

        <div className="field">
          <label htmlFor="torrent-trackers" style={label}>
            Tracker 列表
          </label>
          <textarea
            id="torrent-trackers"
            className="input torrent-trackers"
            rows={4}
            value={trackersText}
            onChange={(event) => setTrackersText(event.target.value)}
            spellCheck={false}
            autoComplete="off"
            disabled={disabled}
            aria-describedby="torrent-trackers-note"
          />
          <p id="torrent-trackers-note" className="torrent-note">
            一行一个，默认留空。留空即只用 DHT / PEX 找分享者；填的地址只会补给公开种子
            （给私有站种子补公共 tracker 会导致封号）。nagare 不内置任何 tracker。
          </p>
        </div>

        <div className="form-actions">
          <button type="submit" className="btn btn--sm" disabled={disabled}>
            {state.phase === 'busy' && state.action === 'save' ? '保存中 …' : '保存'}
          </button>
          {dirty && torrent.enabled && (
            <span className="result result--warn torrent-dirty">有未保存的改动</span>
          )}
        </div>

        {restartRequired && (
          <p className="torrent-restart" role="alert">
            端口 / 映射的改动已保存，<b>重启 nagare 后生效</b>。
          </p>
        )}
      </form>

      <dl className="kv-list">
        <dt>缓存占用</dt>
        <dd style={mono}>{formatBytes(saved.cacheBytes)}</dd>
        <dt>缓存目录</dt>
        <dd style={mono}>{saved.cacheDir === '' ? '（未设置）' : saved.cacheDir}</dd>
      </dl>
      <div className="form-actions">
        <button
          type="button"
          className="btn btn--sm btn--danger"
          onClick={handleClearCache}
          disabled={disabled}
        >
          {state.phase === 'busy' && state.action === 'clear' ? '清空中 …' : '清空缓存'}
        </button>
        <span className="result result--dim torrent-cache-note">
          停止播放即删除该种子的分片，启动与退出时各清空一次；这个按钮用于手动清掉残留。
        </span>
      </div>

      <p
        className={view === null ? 'result' : `result result--${view.tone}`}
        style={mono}
        role="status"
        aria-live="polite"
      >
        {view?.text}
      </p>
    </section>
  )
}

/** 一行一个，去掉首尾空白与空行 */
function parseTrackers(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')
}

/** 合法端口 → 数值；非纯数字或越界 → null（调用方给出 PORT_HINT） */
function parsePort(raw: string): number | null {
  const trimmed = raw.trim()
  if (!/^\d+$/.test(trimmed)) return null
  const value = Number(trimmed)
  return value <= 65535 ? value : null
}

function actionView(state: ActionState): { tone: 'dim' | 'ok' | 'err'; text: string } | null {
  switch (state.phase) {
    case 'idle':
      return null
    case 'busy':
      return { tone: 'dim', text: state.action === 'save' ? '保存中 …' : '正在清空缓存 …' }
    case 'ok':
      return { tone: 'ok', text: state.text }
    case 'error':
      return { tone: 'err', text: state.message }
  }
}
