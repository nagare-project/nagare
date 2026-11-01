import { useState } from 'react'
import { Icon } from '../ui/Icon'
import { formatEpisode } from '../../lib/format'
import type { ContinueItem } from '../../lib/endpoints'

/** 继续观看最多几条（DESIGN.md「媒体库 ①」）：再多就成了第二个媒体库 */
export const MAX_CONTINUE = 3

interface Props {
  items: ContinueItem[]
  onPlay: (fileId: string) => void
  activeFileId: string | null
  pendingFileId: string | null
}

/**
 * 媒体库顶上的「继续观看」：最多三条，16:9 缩略图 + --accent 进度条 +「第 N 集 · 还剩 M 分钟」。
 *
 * 素材只有竖版海报（匹配接口不给横幅，也没有分集缩略图），所以缩略图是把海报按 16:9 裁切 ——
 * object-position 偏上，海报的构图重心通常在上半部分，居中裁会把脸切掉。
 * 封面缺失是常态（只有匹配过的条目才有图）：所有用到图的地方都有无图版式。
 */
export function ContinueSection({ items, onPlay, activeFileId, pendingFileId }: Props) {
  if (items.length === 0) return null
  return (
    <section className="cont" aria-labelledby="continue-heading">
      <h2 id="continue-heading" className="lib-section-title">继续观看</h2>
      <ul className="cont-row">
        {items.slice(0, MAX_CONTINUE).map((item) => (
          <ContinueCard key={item.fileId} item={item} onPlay={onPlay} isActive={item.fileId === activeFileId} isPending={item.fileId === pendingFileId} />
        ))}
      </ul>
    </section>
  )
}

interface CardProps {
  item: ContinueItem
  onPlay: (fileId: string) => void
  isActive: boolean
  isPending: boolean
}

/** 「第 2 集 · 还剩 14 分钟」：没有集号就只说还剩多久，时长未知就只说第几集 */
export function continueMeta(item: ContinueItem): string {
  const episode = item.episode !== null ? `第 ${formatEpisode(item.episode)} 集` : null
  const left = item.durationSec > 0 ? Math.max(1, Math.round((item.durationSec - item.positionSec) / 60)) : null
  return [episode, left !== null ? `还剩 ${left} 分钟` : null].filter(Boolean).join(' · ')
}

function ContinueCard({ item, onPlay, isActive, isPending }: CardProps) {
  const [failed, setFailed] = useState(false)
  const pct = item.durationSec > 0 ? Math.min(100, (item.positionSec / item.durationSec) * 100) : 0
  const meta = continueMeta(item)
  return (
    <li className={isActive ? 'cont-card cont-card--active' : 'cont-card'}>
      <button type="button" className="cont-card-hit" onClick={() => onPlay(item.fileId)} disabled={isPending}>
        <span className="cont-thumb">
          {item.cover !== undefined && !failed ? (
            <img src={item.cover} alt="" onError={() => setFailed(true)} />
          ) : (
            <span className="cont-thumb-blank" aria-hidden="true">流</span>
          )}
          <span className="cont-play" aria-hidden="true"><Icon name="play" size={22} /></span>
          {/* 进度条压在图的下缘：与卡片内容不争空间，又一眼看得出看到哪了 */}
          <span className="cont-bar" aria-hidden="true"><span className="cont-bar-fill" style={{ width: `${pct}%` }} /></span>
        </span>
        <span className="cont-card-text">
          <span className="visually-hidden">继续播放 </span>
          <span className="cont-card-title">{item.title}</span>{' '}
          <span className="cont-card-meta">{meta}</span>
          {item.episodeTitle !== undefined && <>{' '}<span className="cont-card-episode">{item.episodeTitle}</span></>}
          {isActive && <span className="cont-card-state">（正在播放）</span>}
        </span>
      </button>
    </li>
  )
}
