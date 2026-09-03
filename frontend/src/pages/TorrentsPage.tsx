import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { ApiAuthError } from '../lib/api'
import { fetchTorrentStatus, stopTorrent } from '../lib/endpoints'
import { errorText, formatBytes, formatRate } from '../lib/format'
import { mono } from '../theme'
import type { TorrentStatus } from '../lib/endpoints'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/torrents` 磁力任务页。
 *
 * 这一页【没有假数据】—— `/api/torrent/status` 给的就是真实会话状态。
 *
 * 与 seanime 的差别要说清楚，它不是没做完：seanime 列的是一个常驻下载队列，
 * 而 nagare 按决议 M3-4 是「停播即删分片、启动与退出各清空一次缓存」，
 * 压根不存在「任务列表」这个东西。所以这里只可能有一条：当前这个会话。
 * 把它做成队列意味着推翻 M3-4，那是另一件事，不该由一个界面顺手决定。
 */

/** 轮询间隔：与播放流程里的状态条同频，1 秒 */
const POLL_MS = 1000

export function TorrentsPage() {
  const [status, setStatus] = useState<TorrentStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [unauthorized, setUnauthorized] = useState(false)
  const [busy, setBusy] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const poll = useCallback(async () => {
    try {
      const s = await fetchTorrentStatus()
      setStatus(s)
      setError(null)
    } catch (err) {
      if (err instanceof ApiAuthError) {
        setUnauthorized(true)
        return
      }
      // 引擎起不来时这个端点会 503：如实显示，不要静默成「没有任务」
      setError(errorText(err, '读取磁力状态失败'))
      setStatus(null)
    }
  }, [])

  useEffect(() => {
    let alive = true
    const tick = async () => {
      await poll()
      if (alive) timer.current = setTimeout(() => void tick(), POLL_MS)
    }
    void tick()
    return () => {
      alive = false
      if (timer.current !== null) clearTimeout(timer.current)
    }
  }, [poll])

  async function handleStop(): Promise<void> {
    setBusy(true)
    try {
      await stopTorrent()
      await poll()
    } catch (err) {
      setError(errorText(err, '停止失败'))
    } finally {
      setBusy(false)
    }
  }

  if (unauthorized) return <UnauthorizedNotice />

  return (
    <main className="lib-shell">
      <header className="page-head">
        <h1 className="page-title">磁力任务</h1>
      </header>

      {error !== null && (
        <p className="result result--err" style={mono} role="status" aria-live="polite">
          {error}
        </p>
      )}

      {status?.active === true ? (
        <ActiveTorrent status={status} busy={busy} onStop={() => void handleStop()} />
      ) : (
        <IdleNotice />
      )}
    </main>
  )
}

function ActiveTorrent({
  status,
  busy,
  onStop,
}: {
  status: TorrentStatus
  busy: boolean
  onStop: () => void
}) {
  const pct = Math.round(status.progress * 100)
  return (
    <section className="panel torrent-task">
      <header className="torrent-task-head">
        <h2 className="panel-heading">{status.name ?? '（未取到种子名）'}</h2>
        <button type="button" className="btn btn--sm btn--danger" onClick={onStop} disabled={busy}>
          {busy ? '停止中 …' : '停止'}
        </button>
      </header>

      {status.fileName !== undefined && <p className="torrent-file">{status.fileName}</p>}

      {status.error !== undefined && (
        <p className="result result--err" style={mono}>
          {status.error}
        </p>
      )}

      <div className="torrent-bar" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
        <span className="torrent-bar-fill" style={{ width: `${pct}%` }} />
      </div>

      <dl className="torrent-stats" style={mono}>
        <Stat label="进度" value={`${pct}%`} />
        <Stat label="分享者" value={`${status.peers}（做种 ${status.seeders}）`} />
        <Stat label="下载" value={formatRate(status.downRate)} />
        <Stat label="上传" value={formatRate(status.upRate)} />
        <Stat label="缓存占用" value={formatBytes(status.cacheBytes)} />
        <Stat label="持续做种" value={status.seeding ? '开' : '关'} />
      </dl>
    </section>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="torrent-stat">
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  )
}

/**
 * 空态。这里刻意解释「为什么没有任务列表」而不只是说「暂无任务」——
 * 用户从 seanime 过来会以为功能缺了一块。
 */
function IdleNotice() {
  return (
    <div className="page-notice">
      <h2 className="page-notice-title">当前没有磁力会话</h2>
      <p className="page-notice-copy">
        nagare 不维护常驻下载队列：分片只在播放期间存在，停止播放即删除，
        启动与退出还会各清空一次缓存。所以这里最多只会有一条 —— 你正在看的那个。
      </p>
      <p className="page-notice-actions">
        <Link to="/search" className="btn btn--sm">
          去搜索磁力
        </Link>
      </p>
    </div>
  )
}
