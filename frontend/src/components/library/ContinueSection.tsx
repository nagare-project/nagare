import { Icon } from '../ui/Icon'
import { useState } from 'react'
import { formatEpisode } from '../../lib/format'
import type { ContinueItem } from '../../lib/endpoints'

/**
 * 首屏的「继续观看」：一幅横幅 + 一行横向卡片。
 *
 * 形态照着 seanime 的首页做，但素材不一样，这一点决定了实现：
 * animego 的匹配接口给的是【竖版海报】，不是横幅，也没有分集缩略图。
 * 所以横幅是把同一张海报放大、模糊、压暗当背景（它只负责给首屏一点氛围，
 * 不承载信息），卡片则把海报按 16:9 裁切 —— object-position 偏上，
 * 海报的构图重心通常在上半部分，居中裁会把脸切掉。
 *
 * 封面缺失是常态：只有播放过的条目才匹配过、才有图。所有用到图的地方
 * 都必须有无图版式，不能靠"反正都会有图"写死。
 */

interface Props {
  items: ContinueItem[]
  onPlay: (fileId: string) => void
  activeFileId: string | null
  pendingFileId: string | null
}

export function ContinueSection({ items, onPlay, activeFileId, pendingFileId }: Props) {
  if (items.length === 0) return null
  const lead = items[0]!

  return (
    <section className="cont" aria-labelledby="continue-heading">
      <Backdrop cover={lead.cover} />
      <div className="cont-head">
        <p className="cont-kicker" id="continue-heading">
          继续观看
        </p>
        <h2 className="cont-lead-title">{lead.title}</h2>
      </div>
      <ul className="cont-row">
        {items.map((item) => (
          <ContinueCard
            key={item.fileId}
            item={item}
            onPlay={onPlay}
            isActive={item.fileId === activeFileId}
            isPending={item.fileId === pendingFileId}
          />
        ))}
      </ul>
    </section>
  )
}

/**
 * 横幅背景。图加载不出来就什么都不渲染 —— 留一个空的深色块比留一个
 * 破图图标好，而且这一层本来就只是氛围，没有它信息不缺。
 */
function Backdrop({ cover }: { cover?: string }) {
  const [failed, setFailed] = useState(false)
  if (cover === undefined || failed) return null
  return (
    <div className="cont-backdrop" aria-hidden="true">
      <img src={cover} alt="" onError={() => setFailed(true)} />
    </div>
  )
}

interface CardProps {
  item: ContinueItem
  onPlay: (fileId: string) => void
  isActive: boolean
  isPending: boolean
}

function ContinueCard({ item, onPlay, isActive, isPending }: CardProps) {
  const [failed, setFailed] = useState(false)
  const pct =
    item.durationSec > 0 ? Math.min(100, (item.positionSec / item.durationSec) * 100) : 0
  const epLabel =
    item.episode !== null
      ? item.episodeCount > 0
        ? `第 ${formatEpisode(item.episode)} 集 / 共 ${item.episodeCount} 集`
        : `第 ${formatEpisode(item.episode)} 集`
      : ''

  return (
    <li className={isActive ? 'cont-card cont-card--active' : 'cont-card'}>
      <button
        type="button"
        className="cont-card-hit"
        onClick={() => onPlay(item.fileId)}
        disabled={isPending}
        aria-label={`继续播放 ${item.title}${epLabel === '' ? '' : ` ${epLabel}`}`}
      >
        <span className="cont-thumb">
          {item.cover !== undefined && !failed ? (
            <img src={item.cover} alt="" onError={() => setFailed(true)} />
          ) : (
            <span className="cont-thumb-blank" aria-hidden="true">
              流
            </span>
          )}
          <span className="cont-play" aria-hidden="true">
            <Icon name="play" size={23} />
          </span>
          {/* 进度条压在图的下缘：与卡片内容不争空间，又一眼看得出看到哪了 */}
          <span className="cont-bar" aria-hidden="true">
            <span className="cont-bar-fill" style={{ width: `${pct}%` }} />
          </span>
        </span>
      </button>
      <p className="cont-card-title">{item.episodeTitle ?? item.title}</p>
      <p className="cont-card-meta">
        {epLabel}
        {epLabel !== '' && item.episodeTitle !== undefined ? ' · ' : ''}
        {item.episodeTitle !== undefined ? item.title : ''}
      </p>
    </li>
  )
}
