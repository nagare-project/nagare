import { useContext, useLayoutEffect, useState } from 'react'
import { isArtworkLoaded, markArtworkLoaded } from '../../lib/imageCache'
import { ArtworkLoadingContext, useNearViewport } from './artworkLoading'

/**
 * 图片地址变化后可重新加载；无图与错误状态保持原有画幅。
 * 加载时机见 artworkLoading.ts：eager 立刻加载；轮播行里按行给的档；其余离视口近了才加载。
 * reveal：慢慢到的图从透明淡入；挂载时已经加载过、在缓存里的图直接显示（data-loaded="cached"，不淡入），
 * 否则每次重新挂载（换类型筛选、回到发现页）都会「黑一下再显示」。
 */
export function MediaArtwork({ src, title, eager = false, reveal = false }: { src?: string; title: string; eager?: boolean; reveal?: boolean }) {
  const [failedSrc, setFailedSrc] = useState<string>()
  const [loadedSrc, setLoadedSrc] = useState<string>()
  const [instantSrc, setInstantSrc] = useState(() => (src !== undefined && isArtworkLoaded(src) ? src : undefined))
  const [img, setImg] = useState<HTMLImageElement | null>(null)
  const slide = useContext(ArtworkLoadingContext)
  // 轮播行里的卡由行决定（被行视口裁掉的卡观察器永远看不见）
  const near = useNearViewport(img, !eager && slide === undefined)
  // 绘制前看一眼：内存缓存命中的图此刻已经 complete，直接显示
  useLayoutEffect(() => {
    if (!reveal || !src || !img || instantSrc === src) return
    if (isArtworkLoaded(src) || (img.complete && img.naturalWidth > 0)) setInstantSrc(src)
    // 依赖里刻意没有 instantSrc：只在换图或换元素时判断。图慢慢加载完时不能改成「直接显示」，那会打断正在进行的淡入
  }, [img, src, reveal])
  if (!src || src === failedSrc) return <span className="poster-art-mark" aria-hidden="true">{title.slice(0, 1)}</span>
  const loading = eager || slide === 'eager' || near ? 'eager' : 'lazy'
  const revealed = instantSrc === src ? 'cached' : String(loadedSrc === src)
  return <img ref={setImg} src={src} alt="" loading={loading} data-loaded={reveal ? revealed : undefined}
    onLoad={() => { markArtworkLoaded(src); if (reveal) setLoadedSrc(src) }} onError={() => setFailedSrc(src)} />
}
