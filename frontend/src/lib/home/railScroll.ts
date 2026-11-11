// 发现页横排（今日更新、按色调逛）的算术：用鼠标拖动（以及快速拖动后的惯性滑动）、
// 下面的滑块、停在哪里、一打开停在哪里。
//
// 横排是 overflow-x 的一行。触摸本来就能横向滑；鼠标滚轮不行（竖向滚轮滚的是页面），
// 所以桌面上用手拖。横排下面的滑块 —— 一条细轨和一个与屏上所占比例一样宽的滑块 —— 拖、点、
// 键盘都能移动横排。凡是决定一个数字或一个是否的都在这里，不需要 DOM 就能测；
// hook（components/home/useRail.ts）只从元素上读几何、写 scrollLeft。
//
// 纯函数：不碰 React、不碰 DOM。

/** 按下之后指针走过这么远才算拖动；不到这个距离是点了按下去的那张卡 */
export const DRAG_THRESHOLD_PX = 6

/** 只有鼠标左键拖动；触摸和笔走浏览器自己的滚动 */
export function startsDrag(pointerType: string, button: number): boolean {
  return pointerType === 'mouse' && button === 0
}

/**
 * 移动事件到来时左键已经松开了 —— 一次横排没看到松开的按下。拖动门槛之前不捕获指针，
 * 按下后移出横排（取消点击、移到箭头上、右键菜单）在别处松开；下一次悬停不能当成拖动。
 * buttons 是 PointerEvent.buttons（位掩码）。
 */
export function releasedElsewhere(buttons: number): boolean {
  return (buttons & 1) === 0
}

/** 鼠标在横排上的一次按下 */
export interface RailPress {
  readonly pointerId: number
  readonly startX: number
  /** 按下时横排的 scrollLeft */
  readonly startScroll: number
  /** 指针离 startX 最远到过多远（两个方向都算） */
  readonly travel: number
  /** 过了门槛：这次按下是拖动，会移动横排 */
  readonly dragging: boolean
}

export function beginPress(pointerId: number, x: number, scrollLeft: number): RailPress {
  return { pointerId, startX: x, startScroll: scrollLeft, travel: 0, dragging: false }
}

export interface PressMove {
  readonly press: RailPress
  /** 正是这一下移动把按下变成了拖动 */
  readonly started: boolean
  /** 横排该滚到哪里；还不是拖动时为 null */
  readonly scrollLeft: number | null
}

/** 指针移到 x。过了门槛之后横排跟着指针走 —— 往左拖就往后滚 */
export function movePress(press: RailPress, x: number): PressMove {
  const dx = x - press.startX
  const travel = Math.max(press.travel, Math.abs(dx))
  const dragging = press.dragging || travel > DRAG_THRESHOLD_PX
  return {
    press: { ...press, travel, dragging },
    started: dragging && !press.dragging,
    scrollLeft: dragging ? press.startScroll - dx : null,
  }
}

/**
 * 松开之后的那次点击要不要拦下（不打开指针下面那张卡）。变成过拖动的都拦 —— 哪怕又拖回了原处：
 * 读者是在挪横排，不是在选卡片。停住惯性滑动的那一下也拦：像原生的惯性滚动一样，那一下的意思是「停」，
 * 不是「打开滑过去的那张」。（滑回卡片边上的短暂对齐不算惯性：那时点击照常落在读者瞄准的地方。）
 */
export function suppressesClick(press: RailPress, stoppedMotion = false): boolean {
  return press.dragging || stoppedMotion
}

/** 从元素上读到的横排滚动几何 */
export interface RailGeometry {
  /** scrollLeft */
  readonly viewLeft: number
  /** clientWidth */
  readonly viewWidth: number
  /** scrollWidth */
  readonly contentWidth: number
}

/** 横排最远能滚到哪里 */
export function maxScrollLeft(g: RailGeometry): number {
  return Math.max(0, g.contentWidth - g.viewWidth)
}

export interface RailEdges {
  /** 有没有可滚的 */
  readonly overflow: boolean
  readonly atStart: boolean
  readonly atEnd: boolean
}

/** 横排相对两端在哪里。slack 吸收缩放或高分屏报出来的小数滚动位置 */
export function railEdges(g: RailGeometry, slack = 1): RailEdges {
  const max = maxScrollLeft(g)
  return { overflow: max > slack, atStart: g.viewLeft <= slack, atEnd: g.viewLeft >= max - slack }
}

function clamp(value: number, max: number): number {
  return Math.min(max, Math.max(0, value))
}

/**
 * 滑块的 Page Up / Page Down 翻一页：往那个方向大约一屏的卡，落在某张卡的左边贴着内容边（pad，页面留白）的地方，
 * 而不是卡片中间。往后翻，被屏幕右边切到的那张成为第一张，一张都不漏；往前翻对称。starts 是各项在横排里的 offsetLeft。
 */
export function pageScrollLeft(g: RailGeometry, starts: readonly number[], pad: number, direction: 1 | -1): number {
  const span = Math.max(1, g.viewWidth - 2 * pad)
  // 对齐到卡片之前的一整页
  const raw = g.viewLeft + direction * span
  // 每项贴着内容边时是 start - pad。在会移动横排、又不超过一页的那些里取最远的：往后是被右边切到的那张，
  // 往前是还能留一整页在屏上的那张。一个都没有（卡片比屏还宽）：就按整页翻
  const candidates = starts
    .map((s) => s - pad)
    .filter((a) => (direction > 0 ? a <= raw && a > g.viewLeft + 1 : a >= raw && a < g.viewLeft - 1))
  const target = candidates.length === 0 ? raw : direction > 0 ? Math.max(...candidates) : Math.min(...candidates)
  return clamp(target, maxScrollLeft(g))
}

/** 「现在」标记与它两边的卡，横排里的偏移 */
export interface NowLayout {
  /** 标记的 offsetLeft */
  readonly markerStart: number
  /** 最后一张已播的 offsetLeft；还没有已播的时为 null */
  readonly prevStart: number | null
  /** 第一张还没播的右边缘；一张都不剩时是标记的右边缘 */
  readonly nextEnd: number
}

/**
 * 横排一打开滚到哪里：让「现在」标记和下一部要播的在屏上 —— 屏够宽时连同它们前面最后一张已播的。
 * 不用滚就都在屏上时为 null。晚上十一点的读者不该先滚过十五集播完的才找到下一集。
 */
export function nowScrollLeft(g: RailGeometry, now: NowLayout, pad: number): number | null {
  const visibleEnd = (left: number) => left + g.viewWidth - pad
  if (now.nextEnd <= visibleEnd(0)) return null
  const withPrev = now.prevStart === null ? null : now.prevStart - pad
  const target = withPrev !== null && now.nextEnd <= visibleEnd(withPrev) ? withPrev : now.markerStart - pad
  return clamp(target, maxScrollLeft(g))
}

// ── 滑块 ────────────────────────────────────────────────────────────────────

/** 滑块不窄于轨道的这个比例，总能抓得住 */
export const THUMB_MIN_PCT = 6

/** 滑块，按轨道的百分比（0–100） */
export interface Thumb {
  readonly widthPct: number
  readonly leftPct: number
}

/** 横排当前位置对应的滑块：宽度 = 屏上所占比例，位置 = 横排滚了多远。放得下的横排占满整条轨道 */
export function thumbFor(g: RailGeometry, minPct = THUMB_MIN_PCT): Thumb {
  const max = maxScrollLeft(g)
  if (max <= 0 || g.contentWidth <= 0) return { widthPct: 100, leftPct: 0 }
  const widthPct = Math.min(100, Math.max(minPct, (g.viewWidth / g.contentWidth) * 100))
  return { widthPct, leftPct: (clamp(g.viewLeft, max) / max) * (100 - widthPct) }
}

/** 按在滑块上时抓住的是滑块的哪一点（离左边多少 px）：按在滑块上就是指针下面那点（不跳），按在空轨道上是滑块中点（滑块过来） */
export function grabOffset(thumb: Thumb, trackWidth: number, x: number): { onThumb: boolean; grab: number } {
  const width = (thumb.widthPct / 100) * trackWidth
  const left = (thumb.leftPct / 100) * trackWidth
  const onThumb = x >= left && x <= left + width
  return { onThumb, grab: onThumb ? x - left : width / 2 }
}

/** 让滑块的抓点落在指针下（离轨道左边 x px）的 scrollLeft；超出轨道两端就是横排的两端 */
export function scrollForPointer(g: RailGeometry, thumb: Thumb, trackWidth: number, x: number, grab: number): number {
  const room = trackWidth * (1 - thumb.widthPct / 100)
  if (room <= 0) return 0
  const max = maxScrollLeft(g)
  return clamp(((x - grab) / room) * max, max)
}

// ── 停在哪里 ────────────────────────────────────────────────────────────────

/** 横排能停的位置（升序）：每一项（卡片和「现在」标记，按 offsetLeft）贴着留白 pad，以及两端 */
export function railStops(g: RailGeometry, starts: readonly number[], pad: number): number[] {
  const max = maxScrollLeft(g)
  const stops = new Set<number>([0, max])
  for (const start of starts) stops.add(clamp(start - pad, max))
  return [...stops].sort((a, b) => a - b)
}

/** 离 target 最近的停靠点：拖动、惯性、点轨道之后停在这里 */
export function nearestStop(target: number, stops: readonly number[]): number {
  let best = stops[0] ?? 0
  for (const stop of stops) if (Math.abs(stop - target) < Math.abs(best - target)) best = stop
  return best
}

/** 离 target 最近的停靠点的下标 —— 滑块后面的 range 输入数的是停靠点，不是像素，读屏的「加一」就是一张卡 */
export function nearestStopIndex(target: number, stops: readonly number[]): number {
  let best = 0
  stops.forEach((stop, i) => {
    if (Math.abs(stop - target) < Math.abs(stops[best] - target)) best = i
  })
  return best
}

/** 往 direction 方向越过当前位置的下一个停靠点 —— 滑块的方向键。到头就不动 */
export function stepStop(g: RailGeometry, stops: readonly number[], direction: 1 | -1): number {
  if (direction > 0) return stops.find((s) => s > g.viewLeft + 1) ?? maxScrollLeft(g)
  for (let i = stops.length - 1; i >= 0; i--) {
    if (stops[i] < g.viewLeft - 1) return stops[i]
  }
  return 0
}

// ── 惯性滑动 ────────────────────────────────────────────────────────────────

/** 一个时刻的指针位置：PointerEvent 的 clientX 与 timeStamp */
export interface PointerSample {
  readonly t: number
  readonly x: number
}

/** 指针停了这么久才松开，就没有惯性 */
export const FLING_REST_MS = 80
/** 松开时的速度按最后这一段的移动算 */
export const FLING_WINDOW_MS = 120
/** 每 16ms 一帧保留的速度比例 */
export const GLIDE_FRICTION = 0.95
/** 速度（px/ms）低于这个就停下、落到一个停靠点 */
export const GLIDE_MIN_SPEED = 0.08

/**
 * 松开拖动时指针的速度（px/ms，向右为正），按最后 FLING_WINDOW_MS 的移动算；停住过就是 0。
 * 横排的惯性方向相反：往左拖是往后滚。
 */
export function releaseVelocity(samples: readonly PointerSample[], releasedAt: number): number {
  const last = samples[samples.length - 1]
  if (!last || samples.length < 2 || releasedAt - last.t > FLING_REST_MS) return 0
  const first = samples.find((s) => s.t < last.t && last.t - s.t <= FLING_WINDOW_MS)
  return first ? (last.x - first.x) / (last.t - first.t) : 0
}

/** 惯性滑动中的横排上来了一次滚轮，要不要接管：横向（触控板）接管；竖向滚轮滚页面，让惯性自己走完、落到卡上 */
export function takesOverFling(deltaX: number, deltaY: number): boolean {
  return Math.abs(deltaX) > Math.abs(deltaY)
}

/** 惯性的一帧：dt 毫秒里横排走多远，之后剩多少速度 */
export function glideStep(velocity: number, dt: number): { dx: number; velocity: number } {
  return { dx: velocity * dt, velocity: velocity * Math.pow(GLIDE_FRICTION, dt / 16) }
}

// ── 屏上有哪些 ──────────────────────────────────────────────────────────────

/** 一项在横排里的位置：offsetLeft 与 offsetWidth */
export interface ItemBox {
  readonly start: number
  readonly width: number
}

/** 读者在看哪几项（从 1 数，按顺序）：中点落在两侧留白之间的那些。给滑块念的值用；一项都没有时为 null */
export function itemsInView(g: RailGeometry, items: readonly ItemBox[], pad: number): { first: number; last: number } | null {
  const from = g.viewLeft + pad
  const to = g.viewLeft + g.viewWidth - pad
  let first = 0
  let last = 0
  items.forEach((item, i) => {
    const middle = item.start + item.width / 2
    if (middle < from || middle > to) return
    if (first === 0) first = i + 1
    last = i + 1
  })
  return first === 0 ? null : { first, last }
}

/** 进场顺序的最后一档；屏外的都排在这里 */
export const ENTER_ORDER_CAP = 10

/** 横排第一次进入视野时各项浮起的顺序：屏上的从左到右从 0 数，其余（以及超过上限的）都是上限 */
export function enterOrder(g: RailGeometry, items: readonly ItemBox[]): number[] {
  let rank = 0
  return items.map((item) => {
    const middle = item.start + item.width / 2
    const onScreen = middle >= g.viewLeft && middle <= g.viewLeft + g.viewWidth
    if (!onScreen) return ENTER_ORDER_CAP
    const order = Math.min(ENTER_ORDER_CAP, rank)
    rank += 1
    return order
  })
}
