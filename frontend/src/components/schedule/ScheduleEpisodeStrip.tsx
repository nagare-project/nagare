import { useEffect, useId, useRef, useState } from 'react'
import { posterToneStyle } from '../../lib/posterTones'
import { Icon } from '../ui/Icon'
import { MediaArtwork } from '../media/MediaArtwork'
import { MediaPreview } from '../media/MediaPreview'
import { useReducedMotionPreference } from '../media/useReducedMotionPreference'
import type { ScheduleEpisode } from './types'

/**
 * 即将播出的剧集：宽幅卡片横向一条。不自动滚动（DESIGN.md「动效」：除发现页横幅外不做自动轮播），
 * 手指 / 触控板横滑，或点标题右边的前后箭头整屏翻。
 */
export function ScheduleEpisodeStrip({ title, items, now }: { title: string; items: ScheduleEpisode[]; now: Date }) {
  const row = useRef<HTMLUListElement>(null)
  const rowId = useId()
  const headingId = useId()
  const [edges, setEdges] = useState({ start: true, end: true })
  const reduced = useReducedMotionPreference()
  useEffect(() => {
    const element = row.current
    if (!element) return
    const sync = () => setEdges({ start: element.scrollLeft < 1, end: element.scrollLeft + element.clientWidth >= element.scrollWidth - 1 })
    sync()
    element.addEventListener('scroll', sync, { passive: true })
    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(sync)
    resize?.observe(element)
    return () => { element.removeEventListener('scroll', sync); resize?.disconnect() }
  }, [items])
  if (!items.length) return null
  const page = (dir: -1 | 1) => row.current?.scrollBy({ left: dir * row.current.clientWidth, behavior: reduced ? 'instant' : 'smooth' })
  return <section className="schedule-strip" aria-labelledby={headingId}>
    <header className="schedule-section-heading">
      <div><h2 id={headingId}>{title}</h2><p>按本机时区显示</p></div>
      {!(edges.start && edges.end) && <div className="schedule-strip-arrows">
        <button type="button" className="row-arrow" aria-label={`${title}向前翻页`} aria-controls={rowId} disabled={edges.start} onClick={() => page(-1)}><Icon name="left" size={18} /></button>
        <button type="button" className="row-arrow" aria-label={`${title}向后翻页`} aria-controls={rowId} disabled={edges.end} onClick={() => page(1)}><Icon name="right" size={18} /></button>
      </div>}
    </header>
    <ul className="schedule-strip-row" ref={row} id={rowId}>{items.map(({ event, extraCount }) => <li className="schedule-episode" key={event.id} style={posterToneStyle(event.media.posterTones)}>
      <MediaPreview media={event.media} label={null} className="schedule-episode-hit">
        <span className="schedule-episode-art" aria-hidden="true"><MediaArtwork src={event.media.banner ?? event.media.cover} title={event.media.title} /></span>
        <span className="schedule-episode-title">{event.media.title}</span>
        <span className="schedule-episode-meta"><span>第 {event.episode} 集{!!extraCount && ` 及另外 ${extraCount} 集`}</span>
          <time dateTime={event.airingAt}>{timeUntil(event.airingAt, now)}</time>
        </span>
      </MediaPreview>
    </li>)}</ul>
  </section>
}

function timeUntil(airingAt: string, now: Date) {
  const minutes = Math.max(0, Math.ceil((Date.parse(airingAt) - now.getTime()) / 60000))
  if (minutes < 60) return `${minutes} 分钟后`
  if (minutes < 1440) return `${Math.floor(minutes / 60)} 小时后`
  return `${Math.floor(minutes / 1440)} 天后`
}
