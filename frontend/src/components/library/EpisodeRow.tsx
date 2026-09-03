import { formatBytes, formatEpisode, progressPercent } from '../../lib/format'
import type { LibraryItem } from '../../lib/endpoints'
import { mono } from '../../theme'
import './library.css'

/** kind 为这个值时不出徽标（正片是常态，不值得占视觉） */
const KIND_MAIN = 'main'

export interface EpisodeRowProps {
  item: LibraryItem
  /** 点击行尾 ▶ 时回调（fileId 直接透传给 POST /api/play） */
  onPlay: (fileId: string) => void
  /** 该文件正在播放：行加高亮左轨 */
  isActive?: boolean
  /** 播放请求在途：禁用 ▶ 防连点 */
  isPending?: boolean
}

/**
 * 媒体库里的一行：集号（或文件名）· kind 徽标 · 分辨率 · 体积 · 进度 · 播放。
 * 紧凑目录风：有集号时主列画虚线引导线，把编号与右侧元数据连起来。
 */
export function EpisodeRow({ item, onPlay, isActive = false, isPending = false }: EpisodeRowProps) {
  const { episode, fileName, kind, resolution, sizeBytes, progress, fileId } = item
  const kindBadge = kind !== '' && kind.toLowerCase() !== KIND_MAIN ? kind.toUpperCase() : null
  const playLabel = episode !== null ? `播放 第${formatEpisode(episode)}集` : `播放 ${fileName}`

  return (
    <li
      className={isActive ? 'ep-row ep-row--active' : 'ep-row'}
      title={fileName}
      data-file-id={fileId}
    >
      <span className={episode !== null ? 'ep-num' : 'ep-num ep-num--none'} style={mono}>
        {episode !== null ? formatEpisode(episode) : '—'}
      </span>

      <span className="ep-main">
        {episode !== null ? (
          <span className="ep-leader" aria-hidden="true" />
        ) : (
          <span className="ep-name">{fileName}</span>
        )}
        {kindBadge !== null && <span className="badge badge--warn">{kindBadge}</span>}
      </span>

      <span className="ep-res" style={mono}>
        {resolution ?? ''}
      </span>
      <span className="ep-size" style={mono}>
        {formatBytes(sizeBytes)}
      </span>

      <RowProgress progress={progress} />

      <button
        type="button"
        className="ep-play"
        aria-label={playLabel}
        onClick={() => onPlay(fileId)}
        disabled={isPending}
      >
        {'▶︎'}
      </button>
    </li>
  )
}

/** 进度列：看完 → ✓；看了一部分 → 百分比条；没看过 → 占位保持对齐 */
function RowProgress({ progress }: { progress: LibraryItem['progress'] }) {
  if (progress === null) {
    return <span className="ep-progress" aria-hidden="true" />
  }
  if (progress.completed) {
    return (
      <span className="ep-progress">
        <span className="ep-done" title="已看完" aria-label="已看完">
          ✓
        </span>
      </span>
    )
  }
  const pct = progressPercent(progress.positionSec, progress.durationSec)
  return (
    <span className="ep-progress">
      <span
        className="ep-pbar"
        role="progressbar"
        aria-label="观看进度"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={pct}
      >
        <span className="ep-pbar-fill" style={{ width: `${pct}%` }} />
      </span>
      <span className="ep-pct" style={mono}>
        {pct}%
      </span>
    </span>
  )
}
