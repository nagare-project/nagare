import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import type { FocusEvent, RefObject } from 'react'
import {
  canRotate,
  countdownFor,
  countdownKey,
  countdownTimers,
  holdCountdown,
  isHeld,
  nextSlide,
  resumeCountdown,
  type SavedCountdown,
} from '../../lib/home/heroRotation'

// 把横幅的自动切换接到页面上：哪些东西在停住它，以及一段正在走的倒计时要挂的两个定时器。
// 规则本身（有哪些停住、停住对倒计时做什么、什么让它从头算）在 lib/home/heroRotation.ts；这里只报告停住、挂定时器。
//
// 横幅渲染所依赖的值都是 React 状态或 useSyncExternalStore 读的外部状态，在事件回调里设，
// 不在 effect 体里同步设。倒计时本身放在 ref 里：没有东西按它渲染（指示条的填充是被同样的停住暂停的 CSS 动画）。

const REDUCED_MOTION = '(prefers-reduced-motion: reduce)'

// 没有 matchMedia 的环境（测试用的 jsdom）当作没要求减弱动态效果
function subscribeReducedMotion(onChange: () => void): () => void {
  const query = window.matchMedia?.(REDUCED_MOTION)
  if (!query) return () => {}
  query.addEventListener('change', onChange)
  return () => query.removeEventListener('change', onChange)
}

const prefersReducedMotion = () => window.matchMedia?.(REDUCED_MOTION).matches ?? false

function subscribeVisibility(onChange: () => void): () => void {
  document.addEventListener('visibilitychange', onChange)
  return () => document.removeEventListener('visibilitychange', onChange)
}

const tabHidden = () => document.visibilityState === 'hidden'

/**
 * 键盘焦点，不是任何焦点：鼠标点过的按钮留着焦点，要是那也算停住，读者按了「继续」、
 * 鼠标移开之后横幅还是不动。用鼠标的读者由悬停那一条管。
 */
function isKeyboardFocus(el: EventTarget): boolean {
  if (!(el instanceof Element)) return false
  try {
    return el.matches(':focus-visible')
  } catch {
    // 解析不了 :focus-visible 的浏览器：任何焦点都停住
    return true
  }
}

export interface HeroRotation {
  /** 这台设备上横幅会自己切（不止一张、允许动画） */
  rotates: boolean
  /** 此刻有东西停住了它；不自己切时总是 true */
  held: boolean
  /** 读者按了暂停 */
  paused: boolean
  togglePause: () => void
  /** 这一张的 8 秒不经切换就从头算时变（标签页回来了） */
  restarts: number
  /** 挂在横幅根元素上：键盘焦点那一条 */
  rootHandlers: {
    onFocus: (e: FocusEvent<HTMLElement>) => void
    onBlur: (e: FocusEvent<HTMLElement>) => void
  }
}

interface HeroRotationOptions {
  /** 横幅根元素：看它是不是整个滚出了视野 */
  rootRef: RefObject<HTMLElement | null>
  count: number
  current: number
  /** 开始加载第 index 张的横幅图 */
  onPreload: (index: number) => void
  /** 切到第 index 张：时间到了 */
  onAdvance: (index: number) => void
}

export function useHeroRotation({ rootRef, count, current, onPreload, onAdvance }: HeroRotationOptions): HeroRotation {
  const saved = useRef<SavedCountdown | null>(null)
  const [pointer, setPointer] = useState(false)
  const [focus, setFocus] = useState(false)
  const [paused, setPaused] = useState(false)
  const [offscreen, setOffscreen] = useState(false)
  const [restarts, setRestarts] = useState(0)
  const hidden = useSyncExternalStore(subscribeVisibility, tabHidden, tabHidden)
  const reducedMotion = useSyncExternalStore(subscribeReducedMotion, prefersReducedMotion, prefersReducedMotion)

  const rotates = canRotate(count, reducedMotion)
  const held = !rotates || isHeld({ pointer, focus, paused, hidden, offscreen })
  const key = countdownKey(current, restarts)

  // 从后台回来：这一张的 8 秒从头算
  useEffect(() => {
    const onVisibility = () => {
      if (document.visibilityState === 'visible') setRestarts((n) => n + 1)
    }
    document.addEventListener('visibilitychange', onVisibility)
    return () => document.removeEventListener('visibilitychange', onVisibility)
  }, [])

  // 鼠标在横幅上：停住。按「鼠标现在在哪个元素上」判断（整页的 pointerover），不等横幅的 pointerleave：
  // 光标下面的节点被换掉时（暂停按钮点了换图标、「追番」变成「已追」），Chromium 下一次移动可能根本不发
  // 横幅的 pointerleave，等它的停住就永远放不开。新目标上的 pointerover 总会发。
  // 鼠标移出窗口时没有新目标，那由横幅自己的 leave 管。触摸不是悬停。
  useEffect(() => {
    const el = rootRef.current
    if (!el) return
    const over = (e: PointerEvent) => {
      if (e.pointerType !== 'touch') setPointer(e.target instanceof Node && el.contains(e.target))
    }
    const leave = (e: PointerEvent) => {
      if (e.pointerType !== 'touch') setPointer(false)
    }
    document.addEventListener('pointerover', over)
    el.addEventListener('pointerleave', leave)
    return () => {
      document.removeEventListener('pointerover', over)
      el.removeEventListener('pointerleave', leave)
    }
  }, [rootRef])

  // 滚出视野：停住。第一次报告是异步来的，带着横幅当时的位置 —— 恢复到滚动过的位置的重新加载从那里就停住
  useEffect(() => {
    const el = rootRef.current
    if (!el || typeof IntersectionObserver === 'undefined') return
    // 取最后一条：回调跑之前可能排进了两次进出（主线程忙时滚走又滚回），第一条已经过时
    const observer = new IntersectionObserver((entries) => {
      const latest = entries[entries.length - 1]
      if (latest) setOffscreen(!latest.isIntersecting)
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [rootRef])

  const preload = useEffectEvent((index: number) => onPreload(index))
  const advance = useEffectEvent((index: number) => onAdvance(index))

  // 倒计时。用 layout effect：放开停住的那次提交与挂定时器在同一个任务里 —— 页面一显示横幅在走，倒计时就在走
  useLayoutEffect(() => {
    if (!rotates) return
    const start = countdownFor(saved.current, key)
    if (held) {
      saved.current = { key, countdown: start }
      return
    }
    const now = performance.now()
    const running = resumeCountdown(start, now)
    saved.current = { key, countdown: running }
    const { preloadInMs, advanceInMs } = countdownTimers(running, now)
    const next = nextSlide(current, count)
    const preloadTimer = window.setTimeout(() => preload(next), preloadInMs)
    const advanceTimer = window.setTimeout(() => advance(next), advanceInMs)
    return () => {
      window.clearTimeout(preloadTimer)
      window.clearTimeout(advanceTimer)
      saved.current = { key, countdown: holdCountdown(running, performance.now()) }
    }
  }, [rotates, held, key, current, count])

  return {
    rotates,
    held,
    paused,
    togglePause: () => setPaused((p) => !p),
    restarts,
    rootHandlers: {
      onFocus: (e) => setFocus(isKeyboardFocus(e.target)),
      onBlur: (e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setFocus(false)
      },
    },
  }
}
