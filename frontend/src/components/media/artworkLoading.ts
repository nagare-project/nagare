import { createContext, useEffect, useState } from 'react'

/**
 * 封面什么时候开始加载。
 *
 * 浏览器自带的 loading="lazy" 各家距离不同：Chromium 提前约 1200px，WebKit（Safari、macOS 桌面窗口）
 * 要到快露出来才加载 —— 往下滑、往右翻，封面一张张黑着补上来。被轮播行视口裁掉的卡片
 * 两家都不算「在附近」。所以加载时机由页面自己定：
 * - 横向的一行（发现页的今日更新与按色调逛、放送表「即将播出」）给行里每张卡一个档：在加载窗口里的 'eager'，其余 'lazy'；
 * - 其余的图（季度网格、放送表月历……）离视口 NEAR_VIEWPORT_MARGIN 以内就改成 eager。
 *   观察器看不见被横向滚动容器裁掉的部分，所以横向的一行必须走上一条。
 */
export type ArtworkLoading = 'eager' | 'lazy'

/** 轮播行给每张卡的加载档；不在轮播行里时没有值 */
export const ArtworkLoadingContext = createContext<ArtworkLoading | undefined>(undefined)

/** 纵向列表提前多远开始加载（与 Chromium 自带懒加载的距离同量级） */
export const NEAR_VIEWPORT_MARGIN = '1200px 0px'

/** 横向的一行（发现页的横排、放送表「即将播出」）离视口这么近（上下各 1.5 屏）就开始给行里的卡片加载 */
export const ROW_NEAR_MARGIN = '150% 0px'
/** 横向的一行里，屏上这一页之外再往后预先加载几页 */
export const PAGES_AHEAD = 2
/** 还没量出一页几张时按一页 6 张算 */
const FALLBACK_PAGE_SIZE = 6

/**
 * 横向一行的加载窗口右端（不含）：屏上能看到的最后一张，再往后 PAGES_AHEAD 页。
 * 一页的张数就是屏上能看到几张（含露出一角的那张）。窗口只扩不收，由调用方取 max。
 */
export function preloadEnd(inView: number[]): number {
  if (inView.length === 0) return (1 + PAGES_AHEAD) * FALLBACK_PAGE_SIZE
  return Math.max(...inView) + 1 + PAGES_AHEAD * inView.length
}

interface SharedObserver { observer: IntersectionObserver; callbacks: Map<Element, () => void> }

/** 同一个 rootMargin 共用一个观察器：一页几百张封面，不各开一个 */
const observers = new Map<string, SharedObserver>()

function observeOnce(element: Element, margin: string, onNear: () => void): () => void {
  let shared = observers.get(margin)
  if (!shared) {
    const callbacks = new Map<Element, () => void>()
    const observer = new IntersectionObserver((entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        const callback = callbacks.get(entry.target)
        callbacks.delete(entry.target)
        observer.unobserve(entry.target)
        callback?.()
      }
    }, { rootMargin: margin })
    shared = { observer, callbacks }
    observers.set(margin, shared)
  }
  const { observer, callbacks } = shared
  callbacks.set(element, onNear)
  observer.observe(element)
  return () => {
    callbacks.delete(element)
    observer.unobserve(element)
  }
}

/**
 * 元素进入视口外 margin 范围之后返回 true，之后一直是 true（滑走不收回）。
 * 不支持 IntersectionObserver 时一直是 false，由调用方退回浏览器自带的懒加载。
 */
export function useNearViewport(element: Element | null, enabled: boolean, margin: string = NEAR_VIEWPORT_MARGIN): boolean {
  const [near, setNear] = useState(false)
  useEffect(() => {
    if (!enabled || near || !element || typeof IntersectionObserver === 'undefined') return
    return observeOnce(element, margin, () => setNear(true))
  }, [element, enabled, near, margin])
  return near
}

/** 只给测试用：丢掉共用的观察器（测试会换掉全局的 IntersectionObserver） */
export function resetNearViewportForTests() {
  for (const { observer } of observers.values()) observer.disconnect()
  observers.clear()
}
