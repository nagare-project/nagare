import { useEffect, useRef, useState } from 'react'
import { Icon } from '../ui/Icon'
import { MediaArtwork } from '../media/MediaArtwork'
import { MediaPreview } from '../media/MediaPreview'
import { useReducedMotionPreference } from '../media/useReducedMotionPreference'
import type { ScheduleEpisode } from './types'

/** seanime 的宽幅剧集卡：逐卡翻页，指示条与真实滚动位置同步。 */
export function ScheduleEpisodeStrip({ title, items, missing = false, now }: { title: string; items: ScheduleEpisode[]; missing?: boolean; now: Date }) {
  const row = useRef<HTMLUListElement>(null)
  const [positions, setPositions] = useState<number[]>([0])
  const [active, setActive] = useState(0)
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const [paused, setPaused] = useState(false)
  const [visible, setVisible] = useState(false)
  const reduced = useReducedMotionPreference()
  useEffect(() => {
    const element = row.current
    if (!element) return
    const measure = () => {
      const max = Math.max(0, element.scrollWidth - element.clientWidth)
      const cards = [...element.children] as HTMLElement[]
      const stops = [...new Set(cards.map(card => Math.min(max, card.offsetLeft - (cards[0]?.offsetLeft ?? 0))))]
      setPositions(stops.length ? stops : [0])
    }
    measure()
    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure)
    resize?.observe(element)
    const visibility = typeof IntersectionObserver === 'undefined' ? null : new IntersectionObserver(([entry]) => setVisible(entry?.isIntersecting ?? false))
    visibility?.observe(element)
    window.addEventListener('resize', measure)
    return () => { resize?.disconnect(); visibility?.disconnect(); window.removeEventListener('resize', measure) }
  }, [items])
  useEffect(() => {
    const element = row.current
    if (!element) return
    const sync = () => setActive(positions.reduce((best, position, i) => Math.abs(position - element.scrollLeft) < Math.abs(positions[best]! - element.scrollLeft) ? i : best, 0))
    sync()
    element.addEventListener('scroll', sync, { passive: true })
    return () => element.removeEventListener('scroll', sync)
  }, [positions])
  useEffect(() => {
    if (hovered || focused || paused || reduced || !visible || positions.length < 2) return
    const timer = setInterval(() => { if (!document.hidden) row.current?.scrollTo({ left: positions[(active + 1) % positions.length], behavior: 'smooth' }) }, 5000)
    return () => clearInterval(timer)
  }, [hovered, focused, paused, reduced, visible, active, positions])
  if (!items.length) return null
  return <section className="schedule-strip" aria-label={title} onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}
    onFocusCapture={() => setFocused(true)} onBlurCapture={e => { if (!e.currentTarget.contains(e.relatedTarget)) setFocused(false) }}>
    <header className="schedule-section-heading"><div><h2>{missing && <Icon name="lists" size={28} />}{title}</h2>
      {!missing && <p>按本机时区显示近期放送安排</p>}</div>
      {positions.length > 1 && <div className="schedule-strip-dots" aria-label={`${title}翻页`}>
        {positions.map((left, i) => <button type="button" key={left} className={active === i ? 'schedule-strip-dot schedule-strip-dot--on' : 'schedule-strip-dot'} aria-label={`${title}第 ${i + 1} 页`} aria-pressed={active === i}
          onClick={() => row.current?.scrollTo({ left, behavior: reduced ? 'instant' : 'smooth' })} />)}
        {!reduced && <button type="button" className="schedule-strip-pause" aria-label={`${paused ? '播放' : '暂停'}${title}轮播`} aria-pressed={paused} onClick={() => setPaused(!paused)}><Icon name={paused ? 'play' : 'pause'} size={13} /></button>}
      </div>}
    </header>
    <ul className="schedule-strip-row" ref={row}>{items.map(({ event, extraCount }) => <li className="schedule-episode" key={event.id}>
      <MediaPreview media={event.media} className="schedule-episode-hit">
        <span className="schedule-episode-art"><MediaArtwork src={event.media.banner ?? event.media.cover} title={event.media.title} />
          <span className="schedule-episode-hover"><Icon name="info" size={34} /></span></span>
        <span className="schedule-episode-title">{event.media.title}</span>
        <span className="schedule-episode-meta"><span>第 {event.episode} 集{!!extraCount && ` 及另外 ${extraCount} 集`}</span>
          <time dateTime={event.airingAt}>{missing ? new Date(event.airingAt).toLocaleDateString('sv-SE') : timeUntil(event.airingAt, now)}</time>
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
