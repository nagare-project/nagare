import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

/** tokens.css 没加载时（组件测试）的回落值，与 design/tokens.json 的 --dur-banner 一致 */
export const BANNER_INTERVAL_FALLBACK_MS = 8000

/** 横幅切换间隔从 --dur-banner 读：数值只在 design/tokens.json 一处 */
export function bannerInterval(): number {
  if (typeof window === 'undefined') return BANNER_INTERVAL_FALLBACK_MS
  const raw = getComputedStyle(document.documentElement).getPropertyValue('--dur-banner').trim()
  const ms = raw.endsWith('ms') ? parseFloat(raw) : raw.endsWith('s') ? parseFloat(raw) * 1000 : Number.NaN
  return Number.isFinite(ms) && ms > 0 ? ms : BANNER_INTERVAL_FALLBACK_MS
}

/** 标签页在前台、横幅至少露出一点 */
function useOnScreen(element: HTMLElement | null): boolean {
  const [pageVisible, setPageVisible] = useState(() => typeof document === 'undefined' || document.visibilityState !== 'hidden')
  const [inView, setInView] = useState(true)
  useEffect(() => {
    const sync = () => setPageVisible(document.visibilityState !== 'hidden')
    document.addEventListener('visibilitychange', sync)
    return () => document.removeEventListener('visibilitychange', sync)
  }, [])
  useEffect(() => {
    if (!element || typeof IntersectionObserver === 'undefined') return
    const observer = new IntersectionObserver(([entry]) => setInView(entry?.isIntersecting ?? true))
    observer.observe(element)
    return () => observer.disconnect()
  }, [element])
  return pageVisible && inView
}

export interface BannerRotationOptions {
  count: number
  /** 暂时停住：鼠标悬停、键盘焦点在横幅里、横幅打开的对话框还开着。都结束后接着剩下的时间走 */
  held: boolean
  /** 用户按了暂停：一直停到再按 */
  paused: boolean
  /** 系统要求减弱动态效果：完全不自动切 */
  reduced: boolean
  /** 横幅根元素：整个滚出视野时不切 */
  element: HTMLElement | null
}

/**
 * 发现页横幅的自动切换（DESIGN.md「动效」：唯一的自动播放，配齐 WCAG 2.2.2 的暂停规则）：
 * - 每张停 --dur-banner（8 秒）；悬停 / 键盘焦点 / 对话框打开时停住，结束后接着剩下的时间走；
 * - 暂停按钮按下后一直停到再按（同样接着剩下的时间）；
 * - 标签页在后台或横幅滚出视野时不切，回来后当前这张重新给满 8 秒；
 * - 手动切换后，新的那张从头算；
 * - 减弱动态效果时不自动切。
 */
export function useBannerRotation({ count, held, paused, reduced, element }: BannerRotationOptions) {
  const interval = useMemo(bannerInterval, [])
  const [index, setIndex] = useState(0)
  // 手动选同一张时也要重新计时：只靠 index 变化触发不了
  const [restart, setRestart] = useState(0)
  const onScreen = useOnScreen(element)
  const remaining = useRef(interval)
  // 下一次开始计时前先给满一整段：切换之后、手动选择之后、从后台或视野外回来之后
  const fresh = useRef(true)
  const running = count > 1 && !reduced && !held && !paused && onScreen

  useEffect(() => {
    if (!onScreen) fresh.current = true
  }, [onScreen])

  useEffect(() => {
    if (!running) return
    if (fresh.current) {
      remaining.current = interval
      fresh.current = false
    }
    const budget = remaining.current
    const startedAt = Date.now()
    const timer = window.setTimeout(() => {
      fresh.current = true
      setIndex((current) => (current + 1) % count)
    }, budget)
    return () => {
      window.clearTimeout(timer)
      // 停下（悬停、焦点、暂停）时记下还剩多少；恢复时接着走
      remaining.current = Math.max(0, budget - (Date.now() - startedAt))
    }
  }, [running, index, count, interval, restart])

  const select = useCallback((next: number) => {
    if (next < 0 || next >= count) return
    fresh.current = true
    setIndex(next)
    setRestart((n) => n + 1)
  }, [count])

  return { index: count > 0 ? index % count : 0, select, running }
}
