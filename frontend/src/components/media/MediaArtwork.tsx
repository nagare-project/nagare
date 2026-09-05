import { useState } from 'react'

/** 图片地址变化后可重新加载；无图与错误状态保持原有画幅。 */
export function MediaArtwork({ src, title, eager = false, reveal = false }: { src?: string; title: string; eager?: boolean; reveal?: boolean }) {
  const [failedSrc, setFailedSrc] = useState<string>()
  const [loadedSrc, setLoadedSrc] = useState<string>()
  if (!src || src === failedSrc) return <span className="poster-art-mark" aria-hidden="true">{title.slice(0, 1)}</span>
  return <img src={src} alt="" loading={eager ? 'eager' : 'lazy'} data-loaded={reveal ? loadedSrc === src : undefined}
    onLoad={() => { if (reveal) setLoadedSrc(src) }} onError={() => setFailedSrc(src)} />
}
