import { useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import useEmblaCarousel from 'embla-carousel-react'
import { keepArrowFocus } from '../../lib/focus'
import { Icon } from '../ui/Icon'
import { useReducedMotionPreference } from './useReducedMotionPreference'

/**
 * 分页点的上限。点是「按一屏一页」算的（slidesToScroll: 'auto'），本季一百部也只有二十来页；
 * 再多就只画前后箭头 —— 一排几十个点既找不到位置、又会把标题行撑出横向滚动。
 */
export const MAX_ROW_DOTS = 30

interface CarouselRowProps {
  title: string
  children: ReactNode
  /** 标题下面的筛选条（类型标签） */
  filters?: ReactNode
  /** 标题行右侧、箭头左边的链接（如「查看全部」） */
  action?: ReactNode
}

/**
 * 发现页的海报行（DESIGN.md「动效」：海报行不自动滚动）：
 * 只在用户拖动、滑动、滚轮横移或点箭头时动。箭头一直都在 —— 拖拽对语音控制和开关设备用户不可用。
 * 键盘：焦点落到屏外的卡片时 Embla 会把它滚进来；在行里按左右方向键整页翻。
 */
export function CarouselRow({ title, children, filters, action }: CarouselRowProps) {
  const id = useId()
  const headingId = useId()
  const reduced = useReducedMotionPreference()
  const wheel = useRef({ time: 0, direction: 0 })
  const [dragging, setDragging] = useState(false)
  const [snaps, setSnaps] = useState<number[]>([])
  const [selected, setSelected] = useState(0)
  const [edges, setEdges] = useState({ start: true, end: true })
  const prevArrow = useRef<HTMLButtonElement>(null)
  const nextArrow = useRef<HTMLButtonElement>(null)
  const [viewport, api] = useEmblaCarousel({
    align: 'start',
    dragFree: true,
    slidesToScroll: 'auto',
    containScroll: 'trimSnaps',
    // 卡片浮层里打开的对话框（预览、选集）不能被当成拖拽的起点
    watchDrag: (_, event) => !(event.target instanceof Element && event.target.closest('dialog')),
  })

  useEffect(() => {
    if (!api) return
    const sync = () => {
      const next = { start: !api.canScrollPrev(), end: !api.canScrollNext() }
      // Embla 在翻页的同一刻发 select：趁箭头还没变 disabled，把焦点交给另一头
      keepArrowFocus(next, { start: prevArrow.current, end: nextArrow.current })
      setSnaps(api.scrollSnapList())
      setSelected(api.selectedScrollSnap())
      setEdges(next)
    }
    const down = () => setDragging(true)
    const up = () => setDragging(false)
    sync()
    api.on('select', sync).on('reInit', sync).on('pointerDown', down).on('pointerUp', up)
    return () => {
      api.off('select', sync).off('reInit', sync).off('pointerDown', down).off('pointerUp', up)
    }
  }, [api])

  // 触控板横向滑动 / Alt + 滚轮：一次手势翻一页（连续的滚轮事件在 80ms 内只认一次）
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

  const pages = snaps.length
  const showDots = pages > 1 && pages <= MAX_ROW_DOTS
  return (
    <section className="discover-row" aria-labelledby={headingId} data-dragging={dragging || undefined}>
      <div className="row-head">
        <h2 className="row-title" id={headingId}>{title}</h2>
        {action}
        {pages > 1 && (
          <div className="row-controls">
            {showDots && (
              <div className="row-dots" role="group" aria-label={`${title}分页`}>
                {snaps.map((_, index) => (
                  <button type="button" key={index} className="row-dot" aria-label={`${title}第 ${index + 1} 页`}
                    aria-current={selected === index ? 'true' : undefined} aria-controls={id} onClick={() => api?.scrollTo(index, reduced)}>
                    <span />
                  </button>
                ))}
              </div>
            )}
            <button ref={prevArrow} type="button" className="row-arrow" aria-label={`${title}向前翻页`} aria-controls={id} disabled={edges.start} onClick={() => api?.scrollPrev(reduced)}>
              <Icon name="left" size={18} />
            </button>
            <button ref={nextArrow} type="button" className="row-arrow" aria-label={`${title}向后翻页`} aria-controls={id} disabled={edges.end} onClick={() => api?.scrollNext(reduced)}>
              <Icon name="right" size={18} />
            </button>
          </div>
        )}
      </div>
      {filters}
      <div className="discover-row-viewport" ref={viewport} id={id} onKeyDown={(event) => {
        if (event.target instanceof Element && event.target.closest('dialog')) return
        if (event.key === 'ArrowRight') { event.preventDefault(); api?.scrollNext(reduced) }
        if (event.key === 'ArrowLeft') { event.preventDefault(); api?.scrollPrev(reduced) }
      }}>
        <ul className="discover-row-track">{children}</ul>
      </div>
    </section>
  )
}
