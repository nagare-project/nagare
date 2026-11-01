import { useEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'

/** 滚过这么多像素，透明顶栏换成毛玻璃 */
export const GLASS_AFTER_PX = 8
/** 往下【连续】滚这么多像素收起顶栏 */
export const HIDE_AFTER_PX = 12
/** 往上【连续】滚这么多像素滑出顶栏 */
export const SHOW_AFTER_PX = 8

export interface HeaderScrollState {
  /** 已离开页面顶部：画毛玻璃 */
  scrolled: boolean
  /** 顶栏收起（往上平移出视口） */
  hidden: boolean
  /** 这一次出现不要滑动动画（键盘焦点落进已收起的顶栏） */
  instant: boolean
}

/** 焦点是键盘带进来的（:focus-visible）；鼠标点出来的焦点不挡收起。旧环境不认这个伪类时按键盘算，宁可不收。 */
function isKeyboardFocus(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false
  try {
    return target.matches(':focus-visible')
  } catch {
    return true
  }
}

/**
 * 顶栏的滚动状态（DESIGN.md「桌面顶栏 · 状态」）：
 * - 页面顶部透明，滚过 8px 出现毛玻璃；
 * - 往下连续 12px 收起、往上连续 8px 滑出；滚动位置在顶栏高度以内永不收起；
 * - locked（抽屉 / 搜索展开）或键盘焦点在顶栏里时不收起；
 * - 键盘焦点落进已收起的顶栏时立刻出现、不滑。
 *
 * 只订阅 window 的 scroll（passive + 每帧最多算一次）；状态值不变时 React 不会重渲染。
 */
export function useHeaderScroll(header: RefObject<HTMLElement | null>, locked: boolean): HeaderScrollState {
  const [state, setState] = useState<HeaderScrollState>({ scrolled: false, hidden: false, instant: false })
  const lockedRef = useRef(locked)

  useEffect(() => {
    lockedRef.current = locked
    if (locked) setState((prev) => (prev.hidden ? { ...prev, hidden: false, instant: false } : prev))
  }, [locked])

  useEffect(() => {
    let lastY = Math.max(0, window.scrollY)
    let down = 0
    let up = 0
    let frame = 0

    const update = () => {
      frame = 0
      const y = Math.max(0, window.scrollY)
      const delta = y - lastY
      lastY = y
      if (delta > 0) {
        down += delta
        up = 0
      } else if (delta < 0) {
        up -= delta
        down = 0
      }
      const navHeight = header.current?.offsetHeight ?? 0
      // 每次现查焦点：鼠标点到别处、或获得焦点的元素被卸载，都不会留下「还在顶栏里」的陈旧记号
      const focused = document.activeElement
      const keyboardInside = focused !== null && header.current?.contains(focused) === true && isKeyboardFocus(focused)
      const pinned = y <= navHeight || lockedRef.current || keyboardInside
      setState((prev) => {
        const scrolled = y > GLASS_AFTER_PX
        let hidden = prev.hidden
        if (pinned) hidden = false
        else if (down >= HIDE_AFTER_PX) hidden = true
        else if (up >= SHOW_AFTER_PX) hidden = false
        if (scrolled === prev.scrolled && hidden === prev.hidden) return prev
        return { scrolled, hidden, instant: false }
      })
    }
    const onScroll = () => {
      if (frame === 0) frame = window.requestAnimationFrame(update)
    }

    update()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
      if (frame !== 0) window.cancelAnimationFrame(frame)
    }
  }, [header])

  useEffect(() => {
    const element = header.current
    if (!element) return
    const onFocusIn = (event: FocusEvent) => {
      if (!isKeyboardFocus(event.target)) return
      setState((prev) => (prev.hidden ? { ...prev, hidden: false, instant: true } : prev))
    }
    element.addEventListener('focusin', onFocusIn)
    return () => element.removeEventListener('focusin', onFocusIn)
  }, [header])

  return state
}
