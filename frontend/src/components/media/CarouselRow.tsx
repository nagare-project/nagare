import { Children, useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import useEmblaCarousel from 'embla-carousel-react'
import { useArrowFocus } from '../../hooks/useArrowFocus'
import { Icon } from '../ui/Icon'
import { ArtworkLoadingContext, ROW_NEAR_MARGIN, preloadEnd, useNearViewport } from './artworkLoading'
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
 * 封面：行离视口不到 1.5 屏时，屏上这一页与后面两页的卡片立刻加载，往后翻时窗口跟着往后扩、不收回
 * （浏览器自带的懒加载看不见被行视口裁掉的卡，WebKit 里一翻就是一排黑框）。
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
  const [section, setSection] = useState<HTMLElement | null>(null)
  const near = useNearViewport(section, true, ROW_NEAR_MARGIN)
  const [loadUntil, setLoadUntil] = useState(0)
  const prevArrow = useRef<HTMLButtonElement>(null)
  const nextArrow = useRef<HTMLButtonElement>(null)
  // 翻到头时那个箭头变 disabled：焦点交给另一头（不掉到 <body>）
  useArrowFocus(edges, prevArrow, nextArrow)
  const [viewport, api] = useEmblaCarousel({
    align: 'start',
    dragFree: true,
    slidesToScroll: 'auto',
    containScroll: 'trimSnaps',
    // 卡片浮层里打开的对话框（预览、选集）不能被当成拖拽的起点；一屏放得下的行没有可拖的，
    // 不拦的话 Embla 照样让人把卡片拽出去一大截再弹回来
    watchDrag: (emblaApi, event) => (emblaApi.canScrollPrev() || emblaApi.canScrollNext())
      && !(event.target instanceof Element && event.target.closest('dialog')),
  })

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
    return () => {
      api.off('select', sync).off('reInit', sync).off('pointerDown', down).off('pointerUp', up)
    }
  }, [api])

  // 加载窗口：行到了附近才开始；之后屏上能看到的卡变了就往后扩
  useEffect(() => {
    if (!near) return
    const extend = () => setLoadUntil((current) => Math.max(current, preloadEnd(api?.slidesInView() ?? [])))
    extend()
    if (!api) return
    api.on('slidesInView', extend).on('reInit', extend)
    return () => {
      api.off('slidesInView', extend).off('reInit', extend)
    }
  }, [api, near])

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
    <section ref={setSection} className="discover-row" aria-labelledby={headingId} data-dragging={dragging || undefined}>
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
        <ul className="discover-row-track">
          {/* 每张卡套一层加载档（不加 DOM，.discover-row-track > .poster-card 照旧） */}
          {Children.map(children, (child, index) => (
            <ArtworkLoadingContext.Provider value={near && index < loadUntil ? 'eager' : 'lazy'}>{child}</ArtworkLoadingContext.Provider>
          ))}
        </ul>
      </div>
    </section>
  )
}
