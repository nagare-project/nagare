import { useState } from 'react'
import { useArtworkImage } from './useArtworkImage'
import './episode-artwork.css'

/**
 * 优先逐集截图，其次作品横图和海报，与 Seanime 的图片回退顺序一致。
 * 作品页的选集是纵向排的：离视口不远就开始加载（WebKit 的 lazy 要到快露出来才加载，往下滑就是一格格空框），
 * 加载过的图直接显示，慢慢到的在占位底色上淡入。
 */
export function EpisodeArtwork({ image, banner, cover }: { image?: string; banner?: string; cover?: string }) {
  const [failed, setFailed] = useState<string[]>([])
  const src = [image, banner, cover].find((url): url is string => !!url && !failed.includes(url))
  const art = useArtworkImage(src)
  return src
    ? <img ref={art.ref} className="episode-artwork" src={src} alt="" loading={art.loading} data-loaded={art.reveal} onLoad={art.onLoad} onError={() => setFailed(previous => [...previous, src])} />
    : <span className="episode-artwork-empty" aria-hidden="true">暂无图片</span>
}
