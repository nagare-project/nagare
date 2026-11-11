import { useContext, useLayoutEffect, useState } from 'react'
import { isArtworkLoaded, markArtworkLoaded } from '../../lib/imageCache'
import { ArtworkLoadingContext, useNearViewport } from '../media/artworkLoading'

/**
 * 发现页卡片上的封面：从透明淡入，盖在卡片自己的作品色底上；没有地址时画同样大小的空块，网格不变形。
 *
 * 什么时候加载由页面决定（components/media/artworkLoading.ts）：横排给每张卡一个档（屏上这一段和后面两屏是 eager），
 * 不在横排里的离视口不远就改成 eager —— 不依赖浏览器自带的懒加载（WebKit 要到快露出来才加载，被横排裁掉的卡永远不算）。
 * 挂载时已经加载过的图直接显示、不淡入：换一组色调、回到发现页时不会「黑一下再显示」。
 * 加载失败时保持透明，露出作品色底。
 */
export function CoverImage({ src, className }: { src: string | null; className: string }) {
  const [img, setImg] = useState<HTMLImageElement | null>(null)
  const [loadedSrc, setLoadedSrc] = useState<string | null>(null)
  const [instantSrc, setInstantSrc] = useState(() => (src !== null && isArtworkLoaded(src) ? src : null))
  const slot = useContext(ArtworkLoadingContext)
  const near = useNearViewport(img, slot === undefined)
  // 绘制前看一眼：内存缓存命中的图此刻已经 complete，直接显示
  useLayoutEffect(() => {
    if (!src || !img || instantSrc === src) return
    if (isArtworkLoaded(src) || (img.complete && img.naturalWidth > 0)) setInstantSrc(src)
    // 依赖里刻意没有 instantSrc：只在换图或换元素时判断，慢慢到的图不能改成「直接显示」，那会打断正在进行的淡入
  }, [img, src])
  if (!src) return <span aria-hidden="true" className={className} />
  const instant = instantSrc === src
  const visible = instant || loadedSrc === src
  return (
    <img
      ref={setImg}
      src={src}
      alt=""
      className={className}
      loading={slot === 'eager' || near ? 'eager' : 'lazy'}
      decoding="async"
      data-loaded={instant ? 'cached' : String(loadedSrc === src)}
      style={{ opacity: visible ? 1 : 0, transition: instant ? undefined : 'opacity 0.4s ease' }}
      onLoad={() => {
        markArtworkLoaded(src)
        setLoadedSrc(src)
      }}
    />
  )
}
