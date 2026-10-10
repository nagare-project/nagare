import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { DownloadsPanel } from '../components/torrent/DownloadsPanel'
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
 * 这一页【没有假数据】，分两块：
 *   - 上面是边下边播的当前会话（`/api/torrent/status`）。按决议 M3-4 停播即删分片、启动与退出
 *     各清空一次缓存，所以这里最多一条。
 *   - 下面是「下载」（`/api/downloads`）：用户在磁力栏点了「下载」的种子，完整下到下载目录、
 *     下完进媒体库。它走自己的 BT client 与存储，不碰 M3-4 —— 边下边播的语义原样不变。
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

      <DownloadsPanel />
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
 * 边下边播的空态。刻意解释「为什么最多只有一条」，并指向下面的「下载」——
 * 用户从 seanime 过来会以为边下边播的任务也该留下来。
 */
function IdleNotice() {
  return (
    <div className="page-notice">
      <h2 className="page-notice-title">当前没有边下边播的会话</h2>
      <p className="page-notice-copy">
        边下边播的分片只在播放期间存在，停止播放即删除，所以这里最多只会有一条 —— 你正在看的那个。想把整部留下来，就在磁力栏点「下载」，进度在下面。
      </p>
      <p className="page-notice-actions">
        <Link to="/search" className="btn btn--sm">
          去搜索磁力
        </Link>
      </p>
    </div>
  )
}
