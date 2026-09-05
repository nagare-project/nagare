import { useEffect, useId, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import useEmblaCarousel from 'embla-carousel-react'
import Autoplay from 'embla-carousel-autoplay'
import { Icon } from '../ui/Icon'
import { useReducedMotionPreference } from './useReducedMotionPreference'

/** 与 seanime 共用 Embla 的自由拖拽、惯性和逐张分页。 */
export function CarouselRow({ title, children, filters, autoPlay = false, arrows = false }: {
  title: string; children: ReactNode; filters?: ReactNode; autoPlay?: boolean; arrows?: boolean
}) {
  const id = useId()
  const reduced = useReducedMotionPreference()
  const pointerFocus = useRef(false)
  const wheel = useRef({ time: 0, direction: 0 })
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const [paused, setPaused] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [snaps, setSnaps] = useState<number[]>([])
  const [selected, setSelected] = useState(0)
  const [edges, setEdges] = useState({ start: true, end: true })
  const autoplay = useMemo(() => Autoplay({ delay: 5000, playOnInit: false, stopOnInteraction: true, stopOnFocusIn: false }), [])
  const [viewport, api] = useEmblaCarousel({ align: 'start', dragFree: true,
    watchDrag: (_, event) => !(event.target instanceof Element && event.target.closest('dialog')),
  }, [autoplay])
  useEffect(() => {
    if (!api) return
    const sync = () => {
      setSnaps(api.scrollSnapList())
      setSelected(api.selectedScrollSnap())
      setEdges({ start: !api.canScrollPrev(), end: !api.canScrollNext() })
    }
    const down = () => setDragging(true)
    const up = () => setDragging(false)
    sync()
    api.on('select', sync).on('reInit', sync).on('pointerDown', down).on('pointerUp', up)
    return () => { api.off('select', sync).off('reInit', sync).off('pointerDown', down).off('pointerUp', up) }
  }, [api])
  useEffect(() => {
    if (!api) return
    // Embla 在只有一页时不初始化自动播放插件，不能调用 play。
    if (api.scrollSnapList().length > 1 && autoPlay && !reduced && !hovered && !focused && !paused && !dragging) autoplay.play()
    else autoplay.stop()
  }, [api, autoplay, autoPlay, reduced, hovered, focused, paused, dragging, snaps])
  useEffect(() => {
    if (!api) return
    const element = api.rootNode()
    function onWheel(event: WheelEvent) {
      const delta = event.altKey ? event.deltaY : event.deltaX
      if (Math.abs(delta) <= 2 || (!event.altKey && Math.abs(event.deltaY) > Math.abs(delta))) return
      event.preventDefault()
      const direction = Math.sign(delta)
      if (performance.now() - wheel.current.time < 80 && direction === wheel.current.direction) return
      wheel.current = { time: performance.now(), direction }
      if (direction > 0) api!.scrollNext(reduced)
      else api!.scrollPrev(reduced)
    }
    element.addEventListener('wheel', onWheel, { passive: false })
    return () => element.removeEventListener('wheel', onWheel)
  }, [api, reduced])
  return <section className="row discover-row" aria-label={title} data-filtered={!!filters || undefined} data-arrows={arrows || undefined} data-dragging={dragging || undefined}
    onPointerEnter={event => { if (event.pointerType !== 'touch') setHovered(true) }} onPointerLeave={() => setHovered(false)}
    onPointerDownCapture={() => { pointerFocus.current = true; setFocused(false) }}
    onKeyDownCapture={() => { pointerFocus.current = false; setFocused(true) }}
    onFocusCapture={event => setFocused(!pointerFocus.current || !!event.target.closest('dialog[open]'))}
    onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget)) { setFocused(false); pointerFocus.current = false } }}>
    <div className="row-head"><h2 className="row-title">{title}</h2>
      <div className={arrows ? 'row-controls' : 'row-controls row-controls--dots'}>
        {autoPlay && !reduced && snaps.length > 1 && <button type="button" className="row-pause" aria-label={`${paused ? '播放' : '暂停'}${title}轮播`} aria-pressed={paused}
          onClick={() => setPaused(!paused)}><Icon name={paused ? 'play' : 'pause'} size={14} /></button>}
        {arrows ? <>
          <button type="button" className="icon-button" aria-label={`${title}向前翻页`} aria-controls={id} disabled={edges.start} onClick={() => api?.scrollPrev(reduced)}><Icon name="left" size={20} /></button>
          <button type="button" className="icon-button" aria-label={`${title}向后翻页`} aria-controls={id} disabled={edges.end} onClick={() => api?.scrollNext(reduced)}><Icon name="right" size={20} /></button>
        </> : snaps.map((_, index) => <button type="button" key={index} className="row-dot" aria-label={`${title}第 ${index + 1} 页`}
          aria-pressed={selected === index} aria-controls={id} onClick={() => api?.scrollTo(index, reduced)}><span /></button>)}
      </div>
    </div>
    {filters}
    <div className="discover-row-viewport" ref={viewport} id={id} onKeyDown={event => {
      if (event.target instanceof Element && event.target.closest('dialog')) return
      if (event.key === 'ArrowRight') { event.preventDefault(); api?.scrollNext(reduced) }
      if (event.key === 'ArrowLeft') { event.preventDefault(); api?.scrollPrev(reduced) }
    }}><ul className="discover-row-track">{children}</ul></div>
  </section>
}
