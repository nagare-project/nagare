import { useEffect, useEffectEvent, useRef, useState } from 'react'
import type { ChangeEvent, DragEvent, KeyboardEvent, MouseEvent, PointerEvent, RefObject } from 'react'
import {
  ENTER_ORDER_CAP,
  GLIDE_MIN_SPEED,
  beginPress,
  enterOrder,
  glideStep,
  grabOffset,
  itemsInView,
  maxScrollLeft,
  movePress,
  nearestStop,
  nearestStopIndex,
  pageScrollLeft,
  railEdges,
  railStops,
  releaseVelocity,
  releasedElsewhere,
  scrollForPointer,
  startsDrag,
  stepStop,
  suppressesClick,
  takesOverFling,
  thumbFor,
  type ItemBox,
  type PointerSample,
  type RailGeometry,
  type RailPress,
} from '../../lib/home/railScroll'
import { useNearViewport } from '../media/artworkLoading'

// 发现页横排（今日更新、按色调逛的每一组）背后的接线：用鼠标拖（以及快速拖动之后的惯性）、停到卡片上、
// 下面的滑块、横排第一次进入视野时卡片浮起，以及封面什么时候开始加载。
// 决定（什么算拖动、滑块在哪、停在哪）都在 lib/home/railScroll.ts；这些 hook 只从 DOM 读、写 scrollLeft。

export function railGeometry(rail: HTMLElement): RailGeometry {
  return { viewLeft: rail.scrollLeft, viewWidth: rail.clientWidth, contentWidth: rail.scrollWidth }
}

/** 横排的左右内边距：页面留白，卡片对齐的地方 */
export function gutterOf(rail: HTMLElement): number {
  return parseFloat(getComputedStyle(rail).paddingLeft) || 0
}

export function prefersReducedMotion(): boolean {
  return window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
}

/** 横排里每一项（卡片、今日更新的「现在」标记、末尾的面板）从哪里开始 */
function itemStarts(rail: HTMLElement): number[] {
  return Array.from(rail.children, (el) => (el as HTMLElement).offsetLeft)
}

function boxOf(el: HTMLElement): ItemBox {
  return { start: el.offsetLeft, width: el.offsetWidth }
}

/** 只有作品卡（带 data-card）—— 读者数的是它们 */
function cardBoxes(rail: HTMLElement): ItemBox[] {
  return Array.from(rail.querySelectorAll<HTMLElement>(':scope > [data-card]'), boxOf)
}

function stopsOf(rail: HTMLElement, g: RailGeometry): number[] {
  return railStops(g, itemStarts(rail), gutterOf(rail))
}

// ── 停下来，以及惯性 ────────────────────────────────────────────────────────

export interface RailMotion {
  /** 惯性滑动中 */
  flinging: boolean
  /** 读者抓住了横排：惯性或我们自己的平滑滚动原地停下，并说停的是哪一种（null：横排本来就没动） */
  stop: () => StoppedMotion
  /** 以 velocity（px/ms）接着滑，慢下来之后停到卡片上 */
  fling: (velocity: number) => void
  /** 停到最近的停靠点 —— 一张卡贴着留白，或者一端 */
  settle: () => void
  /** 把横排移到 left：要求平滑时平滑（读者要求减弱动态效果时不） */
  scrollTo: (left: number, smooth: boolean) => void
  /** 横排在哪 —— 或者我们的平滑滚动正把它带去哪：滑到一半再按一次键，从那里往下走一步，而不是又瞄准同一张 */
  position: () => number
}

/** 一次按下停住了什么：惯性（这时一下点击的意思是「停」，不是打开滑过去的那张）、我们自己的平滑滚动，或者什么都没有 */
export type StoppedMotion = 'fling' | 'glide' | null

/** 横排这么久没滚动就当平滑滚动结束了（Safari 没有 scrollend） */
const GLIDE_IDLE_MS = 200

/**
 * 读者松手之后自己动横排的东西：一帧帧慢下来的惯性，然后平滑地停到最近的卡上。
 * 横排上一次横向的滚轮（触控板）立刻接管惯性；竖向的滚页面，让惯性自己走完。
 */
export function useRailMotion(railRef: RefObject<HTMLElement | null>): RailMotion {
  const [flinging, setFlinging] = useState(false)
  const frame = useRef(0)
  /** 我们的平滑滚动要去哪，直到到了或者别的东西动了横排 */
  const target = useRef<number | null>(null)
  /** 上一次滚动时离目标多远：平滑滚动只会越来越近 */
  const distance = useRef(Number.POSITIVE_INFINITY)
  const idle = useRef(0)

  const clearTarget = () => {
    target.current = null
    window.clearTimeout(idle.current)
  }

  /** 一段时间没滚动：不管是什么平滑滚动都结束了（Safari 没有 scrollend） */
  const armIdle = () => {
    window.clearTimeout(idle.current)
    idle.current = window.setTimeout(clearTarget, GLIDE_IDLE_MS)
  }

  const scrollTo = (left: number, smooth: boolean) => {
    const rail = railRef.current
    if (!rail) return
    const glide = smooth && !prefersReducedMotion() && Math.abs(rail.scrollLeft - left) >= 1
    if (glide) {
      target.current = left
      distance.current = Math.abs(rail.scrollLeft - left)
      armIdle()
    } else {
      clearTarget()
    }
    rail.scrollTo({ left, behavior: glide ? 'smooth' : 'instant' })
  }

  const settle = () => {
    const rail = railRef.current
    if (!rail) return
    const g = railGeometry(rail)
    const to = nearestStop(g.viewLeft, stopsOf(rail, g))
    if (Math.abs(to - g.viewLeft) >= 1) scrollTo(to, true)
  }

  const cancel = () => {
    if (frame.current !== 0) window.cancelAnimationFrame(frame.current)
    frame.current = 0
  }

  const stop = (): StoppedMotion => {
    const rail = railRef.current
    const stopped: StoppedMotion = frame.current !== 0 ? 'fling' : target.current !== null ? 'glide' : null
    cancel()
    // 平滑滚动原地停下
    if (target.current !== null && rail) rail.scrollTo({ left: rail.scrollLeft, behavior: 'instant' })
    clearTarget()
    setFlinging(false)
    return stopped
  }

  const position = () => target.current ?? railRef.current?.scrollLeft ?? 0

  const fling = (velocity: number) => {
    cancel()
    clearTarget()
    const rail = railRef.current
    if (!rail) return
    if (prefersReducedMotion() || Math.abs(velocity) < GLIDE_MIN_SPEED) {
      setFlinging(false)
      settle()
      return
    }
    setFlinging(true)
    let speed = velocity
    let last = 0
    const step = (now: number) => {
      const dt = last === 0 ? 16 : Math.min(32, Math.max(1, now - last))
      last = now
      const before = rail.scrollLeft
      const next = glideStep(speed, dt)
      rail.scrollLeft = before + next.dx
      speed = next.velocity
      // 速度没耗完就被横排的一端挡住了
      const blocked = Math.abs(next.dx) >= 1 && Math.abs(rail.scrollLeft - before) < 0.5
      if (Math.abs(speed) < GLIDE_MIN_SPEED || blocked) {
        frame.current = 0
        setFlinging(false)
        settle()
        return
      }
      frame.current = window.requestAnimationFrame(step)
    }
    frame.current = window.requestAnimationFrame(step)
  }

  const onWheel = useEffectEvent((e: WheelEvent) => {
    if (frame.current !== 0 && takesOverFling(e.deltaX, e.deltaY)) stop()
  })

  const onScroll = useEffectEvent(() => {
    const rail = railRef.current
    if (target.current === null || !rail) return
    const now = Math.abs(rail.scrollLeft - target.current)
    // 到了 —— 或者离目标反而远了：别的东西（触控板、聚焦把卡片滚进视野）接管了横排
    if (now < 1 || now > distance.current + 1) {
      clearTarget()
      return
    }
    distance.current = now
    armIdle()
  })

  const onScrollEnd = useEffectEvent(() => clearTarget())

  useEffect(() => {
    const rail = railRef.current
    if (!rail) return
    const wheel = (e: WheelEvent) => onWheel(e)
    const scroll = () => onScroll()
    const scrollEnd = () => onScrollEnd()
    rail.addEventListener('wheel', wheel, { passive: true })
    rail.addEventListener('scroll', scroll, { passive: true })
    rail.addEventListener('scrollend', scrollEnd)
    return () => {
      rail.removeEventListener('wheel', wheel)
      rail.removeEventListener('scroll', scroll)
      rail.removeEventListener('scrollend', scrollEnd)
      window.clearTimeout(idle.current)
      if (frame.current !== 0) window.cancelAnimationFrame(frame.current)
    }
  }, [railRef])

  return { flinging, stop, fling, settle, scrollTo, position }
}

// ── 用鼠标拖横排 ────────────────────────────────────────────────────────────

export interface RailDrag {
  /** 正在拖：横排显示抓手，不选中任何东西 */
  dragging: boolean
  handlers: {
    onPointerDown: (e: PointerEvent<HTMLElement>) => void
    onPointerMove: (e: PointerEvent<HTMLElement>) => void
    onPointerUp: (e: PointerEvent<HTMLElement>) => void
    onPointerCancel: (e: PointerEvent<HTMLElement>) => void
    onLostPointerCapture: (e: PointerEvent<HTMLElement>) => void
    onClickCapture: (e: MouseEvent<HTMLElement>) => void
    onDragStart: (e: DragEvent<HTMLElement>) => void
  }
}

interface DragCallbacks {
  /** 横排上按下了：在动它的东西停下，并说停的是什么 */
  onPress: () => StoppedMotion
  /** 松开一次拖动，那一刻横排的速度（px/ms；停住过就是 0） */
  onRelease: (velocity: number) => void
}

/** 记下的移动；速度只看最后 120ms */
const SAMPLE_COUNT = 8

/**
 * 用鼠标按住拖横排。触摸交给浏览器（它本来就会横向滑）。
 *
 * 按下变成拖动之后才捕获指针：一按下就捕获的话，普通的点击会交给横排而不是它下面的卡。
 * 拖动松开后吞掉紧跟着的那一次点击，松手落在卡上不会打开它；并把速度交给 onRelease，快速拖动会接着滑。
 */
export function useRailDrag({ onPress, onRelease }: DragCallbacks): RailDrag {
  const [dragging, setDragging] = useState(false)
  const press = useRef<RailPress | null>(null)
  const samples = useRef<readonly PointerSample[]>([])
  const swallowClick = useRef(false)
  /** 这次按下停住了什么 */
  const stoppedMotion = useRef<StoppedMotion>(null)

  const end = (e: PointerEvent<HTMLElement>) => {
    // 不管怎么结束，都没有在拖的了 —— 包括横排没看到松开的拖动
    setDragging(false)
    const p = press.current
    if (!p || p.pointerId !== e.pointerId) return
    press.current = null
    // 被取消的指针没有点击。松开的指针在同一个任务里收到点击；超时保证一次没来的点击不会吞掉下一次真的
    const stopped = stoppedMotion.current
    stoppedMotion.current = null
    swallowClick.current = e.type === 'pointerup' && suppressesClick(p, stopped === 'fling')
    window.setTimeout(() => {
      swallowClick.current = false
    }, 0)
    if (!p.dragging) {
      // 一下点击停住了横排：从那里停到卡上
      if (stopped !== null) onRelease(0)
      return
    }
    // 横排与指针反方向走
    onRelease(e.type === 'pointerup' ? -releaseVelocity(samples.current, e.timeStamp) : 0)
  }

  return {
    dragging,
    handlers: {
      onPointerDown: (e) => {
        swallowClick.current = false
        setDragging(false)
        if (!startsDrag(e.pointerType, e.button)) {
          press.current = null
          stoppedMotion.current = null
          return
        }
        stoppedMotion.current = onPress()
        press.current = beginPress(e.pointerId, e.clientX, e.currentTarget.scrollLeft)
        samples.current = [{ t: e.timeStamp, x: e.clientX }]
      },
      onPointerMove: (e) => {
        const p = press.current
        if (!p || p.pointerId !== e.pointerId) return
        // 拖动开始之前就在别处松开了（捕获也就没了）：现在是悬停，不是按着
        if (releasedElsewhere(e.buttons)) {
          press.current = null
          setDragging(false)
          return
        }
        const move = movePress(p, e.clientX)
        press.current = move.press
        if (move.started) {
          e.currentTarget.setPointerCapture(e.pointerId)
          // 按下时开始选中的文字清掉；CSS 防止再选
          window.getSelection()?.removeAllRanges()
          setDragging(true)
        }
        if (move.scrollLeft !== null) {
          e.currentTarget.scrollLeft = move.scrollLeft
          samples.current = [...samples.current, { t: e.timeStamp, x: e.clientX }].slice(-SAMPLE_COUNT)
        }
      },
      onPointerUp: end,
      onPointerCancel: end,
      // 捕获被拿走（横排卸载了、浏览器插手了）：按下也结束了。正常松开之后这里找不到按下
      onLostPointerCapture: end,
      onClickCapture: (e) => {
        if (!swallowClick.current) return
        swallowClick.current = false
        e.preventDefault()
        e.stopPropagation()
      },
      // 链接或图片会开始浏览器自己的拖拽（一张卡的残影），那会取消指针、结束我们的拖动
      onDragStart: (e) => e.preventDefault(),
    },
  }
}

// ── 滑块 ────────────────────────────────────────────────────────────────────

/** 滑块要写的两个元素：滑块本身，以及它前面的 range 输入 */
export interface RailSliderParts {
  thumbRef: RefObject<HTMLSpanElement | null>
  inputRef: RefObject<HTMLInputElement | null>
}

export interface RailSlider {
  /** 横排比屏宽：有地方可滑 */
  overflow: boolean
  /** 滑块被按住 */
  dragging: boolean
  /** 指着、按着或聚焦着：滑块整个显出来 */
  active: boolean
  sliderHandlers: {
    onPointerDown: (e: PointerEvent<HTMLElement>) => void
    onPointerMove: (e: PointerEvent<HTMLElement>) => void
    onPointerUp: (e: PointerEvent<HTMLElement>) => void
    onPointerCancel: (e: PointerEvent<HTMLElement>) => void
    onLostPointerCapture: (e: PointerEvent<HTMLElement>) => void
    onPointerEnter: (e: PointerEvent<HTMLElement>) => void
    onPointerLeave: () => void
  }
  inputHandlers: {
    onKeyDown: (e: KeyboardEvent<HTMLInputElement>) => void
    onChange: (e: ChangeEvent<HTMLInputElement>) => void
    onFocus: () => void
    onBlur: () => void
  }
}

interface Hold {
  readonly pointerId: number
  /** 抓住的是滑块的哪一点（离它左边多少 px） */
  readonly grab: number
  readonly startX: number
  /** 指针动过滑块（点轨道只是平滑滚过去） */
  readonly moved: boolean
  /**
   * 手指按在空轨道上：要是不动就抬起，横排滑到这里。不在按下时就滑 —— 这一下可能是竖向翻页的开始，
   * 浏览器会用 pointercancel 接管
   */
  readonly tapGlide: number | null
}

/** 按在滑块上、走动不到这么远的算点了轨道 */
const CLICK_SLOP_PX = 4

/**
 * 横排下面的滑块。拖滑块（抓住的那一点一直在指针下）、点空轨道（横排平滑滚过去、落到卡上），
 * 它前面那个看不见的 range 输入给键盘和辅助技术用：← → 一张卡，Page Up / Page Down 一屏，Home / End 两端。
 *
 * 横排滚动时，滑块的大小位置与输入的值每帧直接写到元素上：为了挪一根条每帧重渲染所有卡是浪费。
 * React 只听会改变画面的结论 —— 有没有可滚的。
 */
export function useRailSlider(
  railRef: RefObject<HTMLElement | null>,
  { thumbRef, inputRef }: RailSliderParts,
  motion: RailMotion,
  valueText: (first: number, last: number, total: number) => string,
): RailSlider {
  const hold = useRef<Hold | null>(null)
  const [overflow, setOverflow] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)

  const describe = useEffectEvent((first: number, last: number, total: number) => valueText(first, last, total))

  useEffect(() => {
    const rail = railRef.current
    if (!rail) return
    let frame = 0
    const measure = () => {
      frame = 0
      const g = railGeometry(rail)
      const thumb = thumbFor(g)
      const bar = thumbRef.current
      if (bar) {
        bar.style.width = `${thumb.widthPct}%`
        bar.style.left = `${thumb.leftPct}%`
      }
      const input = inputRef.current
      if (input) {
        // 数的是停靠点不是像素：读屏的「加一」是一张卡
        const stops = stopsOf(rail, g)
        input.max = String(Math.max(0, stops.length - 1))
        input.value = String(nearestStopIndex(g.viewLeft, stops))
        const cards = cardBoxes(rail)
        const seen = itemsInView(g, cards, gutterOf(rail))
        input.setAttribute('aria-valuetext', seen ? describe(seen.first, seen.last, cards.length) : '')
      }
      const next = railEdges(g).overflow
      setOverflow((prev) => (prev === next ? prev : next))
    }
    const schedule = () => {
      if (frame === 0) frame = window.requestAnimationFrame(measure)
    }
    rail.addEventListener('scroll', schedule, { passive: true })
    // observe() 时也会触发一次，就是第一次测量
    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(schedule)
    if (resize) resize.observe(rail)
    else schedule()
    // 卡片多了、少了（后台刷新换了数据）：横排自己的框不变，内容宽度变了，也要重量
    const children = typeof MutationObserver === 'undefined' ? null : new MutationObserver(schedule)
    children?.observe(rail, { childList: true })
    return () => {
      rail.removeEventListener('scroll', schedule)
      resize?.disconnect()
      children?.disconnect()
      if (frame !== 0) window.cancelAnimationFrame(frame)
    }
  }, [railRef, thumbRef, inputRef])

  const pointerScroll = (e: PointerEvent<HTMLElement>, grab: number): number | null => {
    const rail = railRef.current
    if (!rail) return null
    const track = e.currentTarget.getBoundingClientRect()
    const g = railGeometry(rail)
    return scrollForPointer(g, thumbFor(g), track.width, e.clientX - track.left, grab)
  }

  const release = (e: PointerEvent<HTMLElement>) => {
    const h = hold.current
    if (!h || h.pointerId !== e.pointerId) return
    hold.current = null
    setDragging(false)
    if (h.moved) motion.settle()
    // 手指在空轨道上没动就抬起：原来是一下点按
    else if (h.tapGlide !== null && e.type === 'pointerup') motion.scrollTo(h.tapGlide, true)
  }

  return {
    overflow,
    dragging,
    active: hovered || dragging || focused,
    sliderHandlers: {
      onPointerDown: (e) => {
        const rail = railRef.current
        if (!rail || (e.pointerType === 'mouse' && e.button !== 0)) return
        motion.stop()
        const track = e.currentTarget.getBoundingClientRect()
        const g = railGeometry(rail)
        const x = e.clientX - track.left
        const { onThumb, grab } = grabOffset(thumbFor(g), track.width, x)
        // 空轨道上：横排滑到那里、落在卡上 —— 鼠标立刻滑，手指等抬起且没动过才滑
        const glide = onThumb ? null : nearestStop(scrollForPointer(g, thumbFor(g), track.width, x, grab), stopsOf(rail, g))
        const mouse = e.pointerType === 'mouse'
        hold.current = { pointerId: e.pointerId, grab, startX: e.clientX, moved: false, tapGlide: mouse ? null : glide }
        // 手指先不管，等它横着动：从这里开始的上下翻页是浏览器的（touch-action: pan-y），立刻抓住只会碍事
        if (!mouse) return
        e.currentTarget.setPointerCapture(e.pointerId)
        // 不选中文字，焦点留在原处
        e.preventDefault()
        setDragging(true)
        if (glide !== null) motion.scrollTo(glide, true)
      },
      onPointerMove: (e) => {
        const h = hold.current
        if (!h || h.pointerId !== e.pointerId) return
        if (!h.moved && Math.abs(e.clientX - h.startX) < CLICK_SLOP_PX) return
        // 手指真的在滑滑块时才抓住、显示为按住
        if (!h.moved && e.pointerType !== 'mouse') {
          e.currentTarget.setPointerCapture(e.pointerId)
          setDragging(true)
        }
        hold.current = { ...h, moved: true }
        const left = pointerScroll(e, h.grab)
        if (left !== null && railRef.current) railRef.current.scrollLeft = left
      },
      onPointerUp: release,
      onPointerCancel: release,
      // 手指的按下属于它落下的滑块或轨道（触摸指针隐式捕获），直到 onPointerMove 把它交给滑块。
      // 那时滑块失去捕获、lostpointercapture 冒泡到这里；只有滑块自己失去才算结束，否则第一下移动就松开了
      onLostPointerCapture: (e) => {
        if (e.target === e.currentTarget) release(e)
      },
      onPointerEnter: (e) => {
        if (e.pointerType === 'mouse') setHovered(true)
      },
      onPointerLeave: () => setHovered(false),
    },
    inputHandlers: {
      onKeyDown: (e) => {
        const rail = railRef.current
        if (!rail) return
        // 从正在进行的平滑滚动要去的地方算，第二次按（或按住不放）是往下走，而不是又瞄准同一张
        const g = { ...railGeometry(rail), viewLeft: motion.position() }
        const target =
          e.key === 'ArrowRight' || e.key === 'ArrowUp'
            ? stepStop(g, stopsOf(rail, g), 1)
            : e.key === 'ArrowLeft' || e.key === 'ArrowDown'
              ? stepStop(g, stopsOf(rail, g), -1)
              : e.key === 'PageDown'
                ? pageScrollLeft(g, itemStarts(rail), gutterOf(rail), 1)
                : e.key === 'PageUp'
                  ? pageScrollLeft(g, itemStarts(rail), gutterOf(rail), -1)
                  : e.key === 'Home'
                    ? 0
                    : e.key === 'End'
                      ? maxScrollLeft(g)
                      : null
        if (target === null) return
        e.preventDefault()
        motion.stop()
        motion.scrollTo(target, true)
      },
      // 辅助技术直接设值：一个停靠点的下标
      onChange: (e) => {
        const rail = railRef.current
        if (!rail) return
        const stops = stopsOf(rail, railGeometry(rail))
        const index = Math.round(Number(e.currentTarget.value))
        if (!Number.isFinite(index) || stops.length === 0) return
        motion.stop()
        motion.scrollTo(stops[Math.max(0, Math.min(stops.length - 1, index))], true)
      },
      onFocus: () => setFocused(true),
      onBlur: () => setFocused(false),
    },
  }
}

// ── 进场 ────────────────────────────────────────────────────────────────────

/** 每一项比前一项晚这么久开始（TodayRail.module.css 的 `.rail[data-enter]`） */
const ENTER_STEP_MS = 45
/** 一项浮起要多久（同一条规则） */
const ENTER_RISE_MS = 600
/** 整个进场，连同排在最后的一项，再留一点余量 */
const ENTRANCE_MS = ENTER_ORDER_CAP * ENTER_STEP_MS + ENTER_RISE_MS + 100

/**
 * 横排里的项要不要浮起就位：从横排第一次进入视野那一刻起为 true —— 而且只在页面能用时它不在屏上
 * （按整个视口判断），读者已经看到过的一排不会被弄得闪一下再出来。每项在顺序里的位置写成 --enter-order；
 * 动画是 CSS（减弱动态效果时关掉），自己跑完，不会有东西被留在隐藏状态。跑完之后横排把它去掉：
 * 每播出一部就在新位置重建的「现在」标记，否则会再浮起一次。
 */
export function useRailEntrance(railRef: RefObject<HTMLElement | null>): boolean {
  const [entering, setEntering] = useState(false)

  useEffect(() => {
    const rail = railRef.current
    if (!rail || typeof IntersectionObserver === 'undefined') return
    const box = rail.getBoundingClientRect()
    if (box.bottom > 0 && box.top < window.innerHeight) return
    let finished = 0
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries[entries.length - 1]?.isIntersecting) return
        observer.disconnect()
        const items = Array.from(rail.children) as HTMLElement[]
        const order = enterOrder(railGeometry(rail), items.map(boxOf))
        items.forEach((el, i) => el.style.setProperty('--enter-order', String(order[i])))
        setEntering(true)
        finished = window.setTimeout(() => setEntering(false), ENTRANCE_MS)
      },
      // 往里一点：看得到浮起，而不是在折叠线以下就浮完了
      { rootMargin: '0px 0px -10% 0px' },
    )
    observer.observe(rail)
    return () => {
      observer.disconnect()
      window.clearTimeout(finished)
    }
  }, [railRef])

  return entering
}

// ── 封面什么时候开始加载 ────────────────────────────────────────────────────

/** 横排离视口这么近（上下各 1.5 屏）就开始给它的卡片加载封面 */
export const RAIL_NEAR_MARGIN = '150% 0px'
/** 屏上这一段之外，再往后预先加载几屏 */
export const SCREENS_AHEAD = 2

/** 加载窗口的右边界（px，横排内的坐标）：屏上这一段的右边缘，再往后 SCREENS_AHEAD 屏 */
export function loadEdge(g: RailGeometry): number {
  return g.viewLeft + g.viewWidth * (1 + SCREENS_AHEAD)
}

/** 开始位置落在 edge 之前的卡有几张（按顺序数，卡片是从左到右排的） */
export function cardsBefore(starts: readonly number[], edge: number): number {
  let n = 0
  for (const start of starts) {
    if (start >= edge) break
    n += 1
  }
  return n
}

/**
 * 横排里封面的加载窗口：横排到了视口附近才开始；之后横排滚到哪里窗口就往后扩到哪里，不收回。
 * 返回前几张卡（data-card，按顺序）该立刻加载；还不该加载时是 0。其余的给 lazy ——
 * 但浏览器自带的懒加载看不见被横排视口裁掉的卡（WebKit 里一翻就是一排黑框），所以窗口要自己算。
 * enabled 为假（按色调逛里还没打开过的一组）时不开始。
 */
export function useRailLoadCount(railRef: RefObject<HTMLElement | null>, enabled = true): number {
  const [rail, setRail] = useState<HTMLElement | null>(null)
  useEffect(() => setRail(railRef.current), [railRef])
  const near = useNearViewport(rail, enabled, RAIL_NEAR_MARGIN)
  const [count, setCount] = useState(0)
  useEffect(() => {
    if (!near || !rail || !enabled) return
    let frame = 0
    const extend = () => {
      frame = 0
      const starts = Array.from(rail.querySelectorAll<HTMLElement>(':scope > [data-card]'), (el) => el.offsetLeft)
      const next = cardsBefore(starts, loadEdge(railGeometry(rail)))
      setCount((prev) => (next > prev ? next : prev))
    }
    const schedule = () => {
      if (frame === 0) frame = window.requestAnimationFrame(extend)
    }
    extend()
    rail.addEventListener('scroll', schedule, { passive: true })
    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(schedule)
    resize?.observe(rail)
    return () => {
      rail.removeEventListener('scroll', schedule)
      resize?.disconnect()
      if (frame !== 0) window.cancelAnimationFrame(frame)
    }
  }, [near, rail, enabled])
  return count
}
