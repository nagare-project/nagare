// 发现页横幅的自动切换：规则与算术。
//
// 横幅每 8 秒换到下一张。任何自己动超过五秒的东西都要能停下来（WCAG 2.2.2），所以下面这些会停住它：
//   - 鼠标停在横幅上、键盘焦点在横幅里 —— 两者都结束后接着剩下的时间走；
//   - 暂停按钮，直到再按一次；
//   - 标签页在后台 —— 回来时这一张重新给满 8 秒（读者离开过）；
//   - 横幅整个滚出视野。当前那张给整页上色，没人看得见的切换照样会给读者正在看的那一段换颜色。
// prefers-reduced-motion 时完全不自动切。手动或到点切换后，新的那张从 0 开始算 8 秒。
//
// 停住是暂停倒计时，不是重来：已经走过的时间记下，恢复时接着剩下的走。指示条的填充是
// 跟着同样的停住规则暂停的 CSS 动画，两者对得上，组件不必每帧重渲染。
//
// 纯函数：不碰 React、不碰 DOM，自己没有时钟 —— now 一律由调用方传入。

/** 横幅自动切换时每张停多久（DESIGN.md `--dur-banner`） */
export const HERO_INTERVAL_MS = 8_000

/**
 * 自动切换前多久开始加载下一张的横幅图。横幅图只给快要显示的那张加载；
 * 没有这段提前量，下一张的图会在它自己的淡入之后才到，突然冒出来。
 */
export const HERO_PRELOAD_LEAD_MS = 2_500

/** 所有能停住切换的东西；任一个成立就停住 */
export interface HeroHolds {
  /** 鼠标（或笔）在横幅上；触摸不算悬停 */
  readonly pointer: boolean
  /** 键盘焦点在横幅里 */
  readonly focus: boolean
  /** 读者按了暂停 */
  readonly paused: boolean
  /** 标签页在后台 */
  readonly hidden: boolean
  /** 横幅整个滚出了视野 */
  readonly offscreen: boolean
}

export function isHeld(holds: HeroHolds): boolean {
  return holds.pointer || holds.focus || holds.paused || holds.hidden || holds.offscreen
}

/** 横幅会不会自己切：只有一张时不切，读者要求减弱动态效果时不切 */
export function canRotate(slideCount: number, reducedMotion: boolean): boolean {
  return slideCount > 1 && !reducedMotion
}

/** 当前这张的倒计时 */
export interface Countdown {
  /** 这张已经走过的时间，跨停住累计 */
  readonly spentMs: number
  /** 这一段开始走的时刻；停住时为 null */
  readonly runningSince: number | null
}

export const FRESH_COUNTDOWN: Countdown = { spentMs: 0, runningSince: null }

/** 从 now 开始（或接着）走；已经在走就不变 */
export function resumeCountdown(c: Countdown, now: number): Countdown {
  return c.runningSince === null ? { spentMs: c.spentMs, runningSince: now } : c
}

/** 在 now 停住，记下走过的；已经停住就不变 */
export function holdCountdown(c: Countdown, now: number): Countdown {
  if (c.runningSince === null) return c
  return { spentMs: c.spentMs + Math.max(0, now - c.runningSince), runningSince: null }
}

/** now 时这张还剩多少；不会是负数 */
export function remainingMs(c: Countdown, now: number): number {
  const running = c.runningSince === null ? 0 : Math.max(0, now - c.runningSince)
  return Math.max(0, HERO_INTERVAL_MS - c.spentMs - running)
}

export interface CountdownTimers {
  /** 到开始加载下一张的横幅图 */
  readonly preloadInMs: number
  /** 到切换 */
  readonly advanceInMs: number
}

/** 正在走的倒计时要挂的两个定时器，从 now 量起 */
export function countdownTimers(c: Countdown, now: number): CountdownTimers {
  const advanceInMs = remainingMs(c, now)
  return { preloadInMs: Math.max(0, advanceInMs - HERO_PRELOAD_LEAD_MS), advanceInMs }
}

/** current 的下一张，最后一张之后回到第一张 */
export function nextSlide(current: number, count: number): number {
  return count > 0 ? (current + 1) % count : 0
}

/**
 * 哪一张的倒计时在走：这一张，以及标签页从后台回来过几次。任一个变了都从头算 ——
 * 手动或到点切换，或回到标签页。
 */
export function countdownKey(slide: number, returns: number): string {
  return `${slide}:${returns}`
}

export interface SavedCountdown {
  readonly key: string
  readonly countdown: Countdown
}

/** key 要接着走的倒计时：同一张、同一次回到标签页就用记下的，否则从头算 */
export function countdownFor(saved: SavedCountdown | null, key: string): Countdown {
  return saved !== null && saved.key === key ? saved.countdown : FRESH_COUNTDOWN
}
