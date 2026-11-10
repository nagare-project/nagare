import { createContext, useEffect, useState } from 'react'

/**
 * 封面什么时候开始加载。
 *
 * 浏览器自带的 loading="lazy" 各家距离不同：Chromium 提前约 1200px，WebKit（Safari、macOS 桌面窗口）
 * 要到快露出来才加载 —— 往下滑、往右翻，封面一张张黑着补上来。被轮播行视口裁掉的卡片
 * 两家都不算「在附近」。所以加载时机由页面自己定：
 * - 轮播行（CarouselRow）给行里每张卡一个档：在加载窗口里的 'eager'，其余 'lazy'；
 * - 不在轮播行里的图（季度网格、放送表……）离视口 NEAR_VIEWPORT_MARGIN 以内就改成 eager。
 */
export type ArtworkLoading = 'eager' | 'lazy'

/** 轮播行给每张卡的加载档；不在轮播行里时没有值 */
export const ArtworkLoadingContext = createContext<ArtworkLoading | undefined>(undefined)

/** 纵向列表提前多远开始加载（与 Chromium 自带懒加载的距离同量级） */
export const NEAR_VIEWPORT_MARGIN = '1200px 0px'

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
