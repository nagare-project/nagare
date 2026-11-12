import { useContext, useLayoutEffect, useState } from 'react'
import { isArtworkLoaded, markArtworkLoaded } from '../../lib/imageCache'
import { ArtworkLoadingContext, useNearViewport } from './artworkLoading'

/**
 * 一张封面 / 截图 / 头像的加载时机与淡入状态，所有 <img> 共用（规则见 artworkLoading.ts）：
 * - eager 立刻加载；在横向一行里（ArtworkLoadingContext）听行给的档；其余离视口 NEAR_VIEWPORT_MARGIN 以内才改成 eager。
 *   不靠浏览器自带的 loading="lazy" 决定时机：WebKit（Safari、macOS 桌面窗口）要到快露出来才加载，往下一滑就是一排空框。
 * - reveal：'false' 还在加载（调用方画成透明，露出占位底色）、'true' 刚加载完（淡入）、
 *   'cached' 挂载时已经在缓存里（直接显示，不淡入 —— 否则每次重新挂载都「黑一下再显示」）。
 *
 * ⚠️ 观察器的 rootMargin 只放大视口，不放大中间那层滚动容器：放在自己会滚动的容器（横排、对话框里的列表）里的图，
 * 要么由那一行给档，要么直接 eager。
 */
export type ArtworkReveal = 'cached' | 'true' | 'false'

export interface ArtworkImage {
  ref: (img: HTMLImageElement | null) => void
  loading: 'eager' | 'lazy'
  reveal: ArtworkReveal
  onLoad: () => void
}

export function useArtworkImage(src: string | null | undefined, eager = false): ArtworkImage {
  const [img, setImg] = useState<HTMLImageElement | null>(null)
  const [loadedSrc, setLoadedSrc] = useState<string>()
  const [instantSrc, setInstantSrc] = useState(() => (src && isArtworkLoaded(src) ? src : undefined))
  const slot = useContext(ArtworkLoadingContext)
  // 横排里的卡由行决定（被行视口裁掉的卡观察器永远看不见）
  const near = useNearViewport(img, !eager && slot === undefined)
  // 绘制前看一眼：内存缓存命中的图此刻已经 complete，直接显示
  useLayoutEffect(() => {
    if (!src || !img || instantSrc === src) return
    if (isArtworkLoaded(src) || (img.complete && img.naturalWidth > 0)) setInstantSrc(src)
    // 依赖里刻意没有 instantSrc：只在换图或换元素时判断。图慢慢加载完时不能改成「直接显示」，那会打断正在进行的淡入
  }, [img, src])
  const reveal: ArtworkReveal = !src ? 'false' : instantSrc === src ? 'cached' : loadedSrc === src ? 'true' : 'false'
  return {
    ref: setImg,
    loading: eager || slot === 'eager' || near ? 'eager' : 'lazy',
    reveal,
    onLoad: () => {
      if (!src) return
      markArtworkLoaded(src)
      setLoadedSrc(src)
    },
  }
}
