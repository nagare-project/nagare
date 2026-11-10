import { useEffect, useRef, useState } from 'react'
import { isArtworkLoaded, preloadArtwork } from '../../lib/imageCache'
import { MediaArtwork } from './MediaArtwork'

/** animationend 没来时（标签页在后台、动画被系统取消）最多等这么久就收掉底下的旧图 */
export const HERO_FADE_FALLBACK_MS = 2000

interface Layer { id: number; src: string; title: string; instant: boolean }

/**
 * 发现页横幅的背景图（hero.css「.hero-backdrop-image」）。
 * 切到下一部时，新图先在后台加载、解码完，才作为新的一层盖在旧图上淡入；淡入结束再收掉旧图。
 * 以前是旧图立刻卸掉、新图从透明淡入 —— 新图还没到时整块露出底色，每次切换都「黑一下」。
 * 减弱动态效果时没有淡入，新图到了直接换上，同样不露底色。
 */
export function HeroBackdrop({ src, title, reduced }: { src?: string; title: string; reduced: boolean }) {
  const nextId = useRef(1)
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
    // 加载失败也换上（MediaArtwork 画不出图，露出底色）：总比上一部的图配这一部的标题对
    void preloadArtwork(src).then(() => {
      if (cancelled) return
      setLayers((current) => (current.at(-1)?.src === src ? current : [...current, { id: nextId.current++, src, title, instant: current.length === 0 }]))
    })
    return () => { cancelled = true }
  }, [src, title, topSrc])

  // 盖上去的那层淡入完了（或减弱动态效果、或 animationend 迟迟不来）：只留最上面那层
  const stacked = layers.length > 1
  useEffect(() => {
    if (!stacked) return
    const timer = setTimeout(() => setLayers((current) => current.slice(-1)), reduced ? 0 : HERO_FADE_FALLBACK_MS)
    return () => clearTimeout(timer)
  }, [stacked, reduced, topSrc])

  return layers.map((layer, index) => {
    const isTop = index === layers.length - 1
    return (
      <div key={layer.id} className="hero-backdrop-image" data-instant={layer.instant || undefined}
        onAnimationEnd={isTop && stacked ? () => setLayers((current) => current.slice(-1)) : undefined}>
        <MediaArtwork src={layer.src} title={layer.title} eager />
      </div>
    )
  })
}
