import { useEffect, useRef, useState } from 'react'
import { isArtworkLoaded, preloadArtwork } from '../../lib/imageCache'
import { MediaArtwork } from './MediaArtwork'

/** animationend 没来时（标签页在后台、动画被系统取消）最多等这么久就收掉底下的旧图 */
export const HERO_FADE_FALLBACK_MS = 2000

interface Layer { id: number; src: string; title: string; instant: boolean }

/** 新挂上的图解码完、再过两帧（确实画上屏了）才算到位；量不了就当已经到位 */
function whenPainted(img: HTMLImageElement | null | undefined): Promise<void> {
  const decoded = img && typeof img.decode === 'function' ? img.decode().catch(() => undefined) : Promise.resolve()
  return decoded.then(() => new Promise<void>((resolve) => {
    if (typeof requestAnimationFrame !== 'function') { setTimeout(resolve, 0); return }
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
  }))
}

/**
 * 发现页横幅的背景图（hero.css「.hero-backdrop-image」）。
 * 切到下一部时，新图先在后台加载、解码完（最多等 3 秒，见 imageCache），才作为新的一层盖在旧图上淡入；
 * 淡入结束再收掉旧图。以前是旧图立刻卸掉、新图从透明淡入 —— 新图还没到时整块露出底色，每次切换都「黑一下」。
 * 减弱动态效果时没有淡入：新图画上屏之后才收掉旧图，同样不露底色。
 */
export function HeroBackdrop({ src, title, reduced }: { src?: string; title: string; reduced: boolean }) {
  const nextId = useRef(1)
  const topLayer = useRef<HTMLDivElement>(null)
  // 第一张不等预加载（之前没有图可留）；已经加载过的（回到发现页）直接显示、不淡入
  const [layers, setLayers] = useState<Layer[]>(() => (src ? [{ id: 0, src, title, instant: isArtworkLoaded(src) }] : []))
  const topSrc = layers.at(-1)?.src

  useEffect(() => {
    if (!src) {
      // 这部作品没有图：不能留着上一部的图配这一部的标题
      setLayers((current) => (current.length === 0 ? current : []))
      return
    }
    if (topSrc === src) return
    let cancelled = false
    // 加载失败、超时也换上（MediaArtwork 画不出图，露出底色）：总比上一部的图配这一部的标题对
    void preloadArtwork(src).then(() => {
      if (cancelled) return
      const layer: Layer = { id: nextId.current++, src, title, instant: false }
      setLayers((current) => (current.at(-1)?.src === src ? current : [...current, layer]))
    })
    return () => { cancelled = true }
  }, [src, title, topSrc])

  // 盖上去的那层到位了：只留最上面那层。淡入时等 animationend（下面的 onAnimationEnd）；
  // 减弱动态效果时等新图画上屏；两样都等不来（标签页在后台、动画被系统取消）就到兜底时间收
  const stacked = layers.length > 1
  useEffect(() => {
    if (!stacked) return
    let cancelled = false
    const settle = () => { if (!cancelled) setLayers((current) => current.slice(-1)) }
    const timer = setTimeout(settle, HERO_FADE_FALLBACK_MS)
    if (reduced) void whenPainted(topLayer.current?.querySelector('img')).then(settle)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [stacked, reduced, topSrc])

  return layers.map((layer, index) => {
    const isTop = index === layers.length - 1
    return (
      <div key={layer.id} ref={isTop ? topLayer : undefined} className="hero-backdrop-image" data-instant={layer.instant || undefined}
        onAnimationEnd={isTop && stacked ? () => setLayers((current) => current.slice(-1)) : undefined}>
        <MediaArtwork src={layer.src} title={layer.title} eager />
      </div>
    )
  })
}
