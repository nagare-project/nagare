import { useEffect, useId, useRef, useState } from 'react'
import { m } from 'motion/react'
import { Icon } from '../ui/Icon'
import { useReducedMotionPreference } from './useReducedMotionPreference'

export const GENRES = ['全部', '动作', '冒险', '喜剧', '剧情', '卖肉', '奇幻', '恐怖', '魔法少女', '机战', '音乐', '悬疑', '心理', '恋爱', '科幻', '日常', '运动', '超自然', '惊悚']

const API_GENRES = ['', 'Action', 'Adventure', 'Comedy', 'Drama', 'Ecchi', 'Fantasy', 'Horror', 'Mahou Shoujo', 'Mecha', 'Music', 'Mystery', 'Psychological', 'Romance', 'Sci-Fi', 'Slice of Life', 'Sports', 'Supernatural', 'Thriller']
export const genreQuery = (label: string) => API_GENRES[GENRES.indexOf(label)] ?? label
export const genreLabel = (query: string) => GENRES[API_GENRES.indexOf(query)] ?? '全部'

/** 原版类型栏：内容宽度标签、滑动选中背景、500px 翻页与惯性拖拽。 */
export function DiscoverGenres({ title, value, onChange }: { title: string; value: string; onChange: (value: string) => void }) {
  const id = useId()
  const scroll = useRef<HTMLDivElement>(null)
  const frame = useRef(0)
  const drag = useRef({ active: false, moved: false, x: 0, last: 0, time: 0, speed: 0, start: 0 })
  const [edges, setEdges] = useState({ start: true, end: true })
  const reduced = useReducedMotionPreference()
  useEffect(() => {
    const element = scroll.current!
    const sync = () => setEdges({ start: element.scrollLeft < 1, end: element.scrollLeft + element.clientWidth >= element.scrollWidth - 1 })
    sync()
    element.addEventListener('scroll', sync, { passive: true })
    const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(sync)
    observer?.observe(element)
    return () => { observer?.disconnect(); element.removeEventListener('scroll', sync); cancelAnimationFrame(frame.current) }
  }, [])
  function finish(cancelled = false) {
    const state = drag.current
    if (!state.active) return
    state.active = false
    if (cancelled || reduced || !state.moved || performance.now() - state.time > 80) return
    let previous = performance.now()
    const tick = (now: number) => {
      const element = scroll.current
      if (!element) return
      const elapsed = Math.min(32, now - previous)
      previous = now
      const before = element.scrollLeft
      element.scrollLeft -= state.speed * elapsed
      state.speed *= Math.pow(.95, elapsed / 16.67)
      if (Math.abs(state.speed) > .02 && element.scrollLeft !== before) frame.current = requestAnimationFrame(tick)
    }
    frame.current = requestAnimationFrame(tick)
  }
  return <div className="discover-genre-strip">
    <div className="discover-genre-scroll" ref={scroll} id={id}
      onPointerDown={event => {
        if (event.pointerType === 'touch' || event.button !== 0) return
        cancelAnimationFrame(frame.current)
        drag.current = { active: true, moved: false, x: event.clientX, last: event.clientX, time: performance.now(), speed: 0, start: event.currentTarget.scrollLeft }
      }}
      onPointerMove={event => {
        const state = drag.current
        if (!state.active) return
        if (!state.moved && Math.abs(event.clientX - state.x) < 20) return
        state.moved = true
        event.currentTarget.setPointerCapture(event.pointerId)
        const now = performance.now()
        state.speed = (event.clientX - state.last) / Math.max(1, now - state.time)
        state.last = event.clientX
        state.time = now
        event.currentTarget.scrollLeft = state.start - (event.clientX - state.x)
      }} onPointerUp={() => finish()} onPointerCancel={() => finish(true)} onLostPointerCapture={() => finish()}
      onPointerLeave={() => { if (!drag.current.moved) finish(true) }}
      onClickCapture={event => { if (drag.current.moved) { event.preventDefault(); event.stopPropagation(); drag.current.moved = false } }}>
      <nav className="discover-genres" aria-label={`${title}类型`}>
        {GENRES.map(genre => <button key={genre} type="button" aria-pressed={value === genre} onClick={() => onChange(genre)}
          onFocus={event => {
            const element = scroll.current!
            const left = event.currentTarget.offsetLeft
            if (left < element.scrollLeft + 32 || left + event.currentTarget.offsetWidth > element.scrollLeft + element.clientWidth - 32) {
              element.scrollTo({ left: left - element.clientWidth / 2, behavior: reduced ? 'instant' : 'smooth' })
            }
          }}>
          {value === genre && <m.span className="discover-genre-pill" layoutId={`genre-${id}`} transition={reduced ? { duration: 0 } : { type: 'spring', stiffness: 500, damping: 38 }} />}
          <span>{genre}</span>
        </button>)}
      </nav>
    </div>
    {(['start', 'end'] as const).map(side => <button key={side} type="button" className={`discover-genre-arrow discover-genre-arrow--${side}`}
      aria-label={`${title}类型向${side === 'start' ? '前' : '后'}翻页`} aria-controls={id} disabled={edges[side]}
      onClick={() => { cancelAnimationFrame(frame.current); scroll.current?.scrollBy({ left: side === 'start' ? -500 : 500, behavior: reduced ? 'instant' : 'smooth' }) }}>
      <Icon name={side === 'start' ? 'left' : 'right'} size={28} />
    </button>)}
  </div>
}
