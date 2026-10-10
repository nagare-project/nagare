import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchDownloads, removeDownload } from '../../lib/endpoints'
import type { DownloadsData, DownloadTask } from '../../lib/endpoints'
import { errorText, formatBytes, formatRate } from '../../lib/format'
import { mono } from '../../theme'
import './downloads.css'

/** 有在下的任务时每 2 秒刷新一次；都停了就放慢到 15 秒（别处加的新任务也能看到） */
const ACTIVE_POLL_MS = 2000
const IDLE_POLL_MS = 15000

const STATE_LABELS: Record<DownloadTask['state'], string> = {
  metadata: '正在找分享者',
  downloading: '下载中',
  done: '已完成',
  failed: '失败',
}

/**
 * 磁力任务页的「下载」：用户在磁力栏点了「下载」的种子，完整下到下载目录，下完自动进媒体库。
 *
 * 与页面上方那条边下边播会话是两回事 —— 那条停播即删（决议 M3-4），这里是用户明确要留下来的，
 * 活过重启、没下完的下次启动接着下。
 */
export function DownloadsPanel() {
  const [data, setData] = useState<DownloadsData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback(async (): Promise<DownloadsData | null> => {
    try {
      const next = await fetchDownloads()
      setData(next)
      setError(null)
      return next
    } catch (err) {
      setError(errorText(err, '读取下载列表失败'))
      return null
    }
  }, [])

  useEffect(() => {
    let alive = true
    const tick = async () => {
      const next = await load()
      if (!alive) return
      const active = next?.downloads.some(task => task.state === 'metadata' || task.state === 'downloading') ?? false
      timer.current = setTimeout(() => void tick(), active ? ACTIVE_POLL_MS : IDLE_POLL_MS)
    }
    void tick()
    return () => {
      alive = false
      if (timer.current !== null) clearTimeout(timer.current)
    }
  }, [load])

  async function remove(task: DownloadTask): Promise<void> {
    setBusyId(task.id)
    try {
      await removeDownload(task.id)
      await load()
    } catch (err) {
      setError(errorText(err, '删除下载失败'))
    } finally {
      setBusyId(null)
    }
  }

  const downloads = data?.downloads ?? []
  return (
    <section className="panel downloads-panel" aria-labelledby="downloads-heading">
      <header className="downloads-head">
        <h2 id="downloads-heading" className="panel-heading">下载</h2>
        {data && <span className="downloads-dir" style={mono} title="在设置页的「磁力」里可以改">下载目录：{data.dir}</span>}
      </header>
      {error !== null && <p className="result result--err" role="alert">{error}</p>}
      {data !== null && downloads.length === 0 && (
        <p className="downloads-empty">
          还没有下载。在作品页的「磁力播放」里点某个版本旁边的「下载」，整个种子会完整下到上面的目录，下完自动出现在媒体库里。
        </p>
      )}
      {downloads.length > 0 && (
        <ul className="download-list">
          {downloads.map(task => <DownloadRow key={task.id} task={task} busy={busyId === task.id} onRemove={() => void remove(task)} />)}
        </ul>
      )}
    </section>
  )
}

function DownloadRow({ task, busy, onRemove }: { task: DownloadTask; busy: boolean; onRemove: () => void }) {
  const active = task.state === 'metadata' || task.state === 'downloading'
  const pct = task.size ? Math.min(100, Math.floor((task.bytesDone / task.size) * 100)) : 0
  const status = task.state === 'downloading' ? `${STATE_LABELS.downloading} ${pct}%` : STATE_LABELS[task.state]
  const details = active
    ? [task.size ? `${formatBytes(task.bytesDone)} / ${formatBytes(task.size)}` : null, task.state === 'downloading' ? formatRate(task.downRate) : null, `分享者 ${task.peers}`]
    : [task.size ? formatBytes(task.size) : null]
  const removeLabel = active ? '取消并删除已下载部分' : task.state === 'done' ? '从列表移除' : '删除'
  return (
    <li className={`download-item download-item--${task.state}`}>
      <div className="download-main">
        <strong className="download-name" title={task.title}>{task.name ?? task.title}</strong>
        <span className={`download-state download-state--${task.state}`}>{status}</span>
      </div>
      {active && (
        <div className="torrent-bar" role="progressbar" aria-label={`${task.name ?? task.title} 下载进度`} aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
          <span className="torrent-bar-fill" style={{ width: `${pct}%` }} />
        </div>
      )}
      <p className="download-details" style={mono}>{details.filter(Boolean).join(' · ')}</p>
      {task.state === 'metadata' && task.peers === 0 && (
        <p className="download-hint">还没连上分享这个种子的人。老种子可能要等一阵；一直是 0 就说明可能已经没人做种了。</p>
      )}
      {task.state === 'done' && task.paths && task.paths.length > 0 && (
        <p className="download-details" style={mono}>{task.paths.join('\n')}</p>
      )}
      {task.error && <p className="result result--err">{task.error}</p>}
      <div className="download-actions">
        {task.state === 'done' && <a className="btn btn--sm" href="/">去媒体库</a>}
        <button type="button" className={active ? 'btn btn--sm btn--danger' : 'btn btn--sm'} disabled={busy} onClick={onRemove}>
          {busy ? '处理中…' : removeLabel}
        </button>
      </div>
    </li>
  )
}
