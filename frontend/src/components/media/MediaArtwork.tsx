import { useState } from 'react'
import { useArtworkImage } from './useArtworkImage'

/**
 * 图片地址变化后可重新加载；无图与错误状态保持原有画幅。
 * 加载时机见 artworkLoading.ts：eager 立刻加载；轮播行里按行给的档；其余离视口近了才加载。
 * reveal：慢慢到的图从透明淡入；挂载时已经加载过、在缓存里的图直接显示（data-loaded="cached"，不淡入），
 * 否则每次重新挂载（换类型筛选、回到发现页）都会「黑一下再显示」。
 */
export function MediaArtwork({ src, title, eager = false, reveal = false }: { src?: string; title: string; eager?: boolean; reveal?: boolean }) {
  const [failedSrc, setFailedSrc] = useState<string>()
  const art = useArtworkImage(src, eager)
  if (!src || src === failedSrc) return <span className="poster-art-mark" aria-hidden="true">{title.slice(0, 1)}</span>
  return <img ref={art.ref} src={src} alt="" loading={art.loading} data-loaded={reveal ? art.reveal : undefined}
    onLoad={art.onLoad} onError={() => setFailedSrc(src)} />
}
