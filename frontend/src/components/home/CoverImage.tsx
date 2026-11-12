import { useArtworkImage } from '../media/useArtworkImage'

/**
 * 发现页卡片上的封面：从透明淡入，盖在卡片自己的作品色底上；没有地址时画同样大小的空块，网格不变形。
 *
 * 什么时候加载由页面决定（components/media/artworkLoading.ts）：横排给每张卡一个档（屏上这一段和后面两屏是 eager），
 * 不在横排里的离视口不远就改成 eager —— 不依赖浏览器自带的懒加载（WebKit 要到快露出来才加载，被横排裁掉的卡永远不算）。
 * 挂载时已经加载过的图直接显示、不淡入：换一组色调、回到发现页时不会「黑一下再显示」。
 * 加载失败时保持透明，露出作品色底。
 */
export function CoverImage({ src, className }: { src: string | null; className: string }) {
  const art = useArtworkImage(src)
  if (!src) return <span aria-hidden="true" className={className} />
  const instant = art.reveal === 'cached'
  const visible = art.reveal !== 'false'
  return (
    <img
      ref={art.ref}
      src={src}
      alt=""
      className={className}
      loading={art.loading}
      decoding="async"
      data-loaded={art.reveal}
      style={{ opacity: visible ? 1 : 0, transition: instant ? undefined : 'opacity 0.4s ease' }}
      onLoad={art.onLoad}
    />
  )
}
