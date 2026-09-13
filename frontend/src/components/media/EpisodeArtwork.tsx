import { useState } from 'react'

/** 优先逐集截图，其次作品横图和海报，与 Seanime 的图片回退顺序一致。 */
export function EpisodeArtwork({ image, banner, cover }: { image?: string; banner?: string; cover?: string }) {
  const [failed, setFailed] = useState<string[]>([])
  const src = [image, banner, cover].find((url): url is string => !!url && !failed.includes(url))
  return src ? <img className="episode-artwork" src={src} alt="" loading="lazy" onError={() => setFailed(previous => [...previous, src])} /> : <span className="episode-artwork-empty" aria-hidden="true">暂无图片</span>
}
