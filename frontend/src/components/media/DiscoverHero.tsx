import { useEffect, useRef, useState } from 'react'
import { preloadArtwork } from '../../lib/imageCache'
import { posterToneStyle } from '../../lib/posterTones'
import { Icon } from '../ui/Icon'
import { DiscoverTrailer } from './DiscoverTrailer'
import { genreLabel } from './DiscoverGenres'
import { HeroBackdrop } from './HeroBackdrop'
import { MediaArtwork } from './MediaArtwork'
import { MediaEntryLink } from './MediaEntryLink'
import { MediaPreview } from './MediaPreview'
import { useBannerRotation } from './useBannerRotation'
import { useDesktopLayout } from './useDesktopLayout'
import { useMediaTrailer } from './useMediaTrailer'
import { useReducedMotionPreference } from './useReducedMotionPreference'
import type { MediaSummary } from './types'

const STATUS_LABEL: Record<string, string> = { RELEASING: '放送中', FINISHED: '已完结', NOT_YET_RELEASED: '未播出' }

/** 横幅上那一行元信息：评分 · 放送状态 · 集数 · 类型（数字走等宽字体） */
function heroMeta(media: MediaSummary): { score?: string; parts: string[] } {
  const parts: string[] = []
  const status = media.status ? STATUS_LABEL[media.status] : undefined
  if (status) parts.push(status)
  if (media.format === 'MOVIE') parts.push('剧场版')
  else if (media.nextAiring) parts.push(`已播 ${Math.max(0, media.nextAiring.episode - 1)} 集`)
  else if (media.episodes) parts.push(`共 ${media.episodes} 集`)
  for (const genre of media.genres.slice(0, 3)) {
    const label = genreLabel(genre)
    parts.push(label === '全部' ? genre : label)
  }
  return { score: media.score ? (media.score / 10).toFixed(1) : undefined, parts }
}

/**
 * 发现页横幅（DESIGN.md「发现」「动效」）：每 8 秒换一部，配齐暂停规则；
 * 暂停按钮常驻（触屏 44px），减弱动态效果时不自动切、也不显示暂停按钮。
 * 悬停一秒后（只在宽屏、鼠标）背景换成静音预告片。
 * 下一部的背景与封面提前加载好，切过去时不等图（HeroBackdrop 再兜一层：图没到就先留着旧图）。
 */
export function DiscoverHero({ items: latest, label }: { items: MediaSummary[]; label: string }) {
  const [element, setElement] = useState<HTMLElement | null>(null)
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [userPaused, setUserPaused] = useState(false)
  // 横幅停住（悬停、键盘焦点在里面、预览对话框开着）时钉住这一组作品：后台刷新换了数据（换序、换作品），
  // 当前这张不能在用户眼前被换掉 —— 预览对话框与键盘焦点都挂在这张上，一换就被卸掉。停住结束后再换成新数据
  const held = hovered || focused || dialogOpen
  const [pinned, setPinned] = useState<MediaSummary[] | null>(null)
  if (held && pinned === null) setPinned(latest)
  if (!held && pinned !== null) setPinned(null)
  const items = pinned ?? latest
  // 鼠标点过留下的焦点不算「键盘焦点」：只有键盘（或程序）把焦点移进来才停住
  const pointerFocus = useRef(false)
  const desktop = useDesktopLayout()
  const reduced = useReducedMotionPreference()
  const count = items.length
  const { index, select, running } = useBannerRotation({ count, held, paused: userPaused, reduced, element })
  const media = items[index]
  const trailerActive = desktop && hovered && !dialogOpen && !reduced
  const trailerId = useMediaTrailer(media, trailerActive)
  const upcoming = count > 1 ? items[(index + 1) % count] : undefined
  const upcomingBackdrop = upcoming ? upcoming.banner ?? upcoming.cover : undefined
  const upcomingCover = upcoming?.cover
  useEffect(() => {
    if (upcomingBackdrop) void preloadArtwork(upcomingBackdrop)
    if (upcomingCover && upcomingCover !== upcomingBackdrop) void preloadArtwork(upcomingCover)
  }, [upcomingBackdrop, upcomingCover])
  if (!media) return null
  const meta = heroMeta(media)

  return (
    <section ref={setElement} className="hero" aria-roledescription="轮播" aria-label="精选作品" style={posterToneStyle(media.posterTones)}
      onPointerEnter={(event) => { if (event.pointerType !== 'touch') setHovered(true) }}
      onPointerLeave={() => setHovered(false)}
      onPointerDownCapture={() => { pointerFocus.current = true; setFocused(false) }}
      onKeyDownCapture={() => { pointerFocus.current = false }}
      onFocusCapture={() => { if (!pointerFocus.current) setFocused(true) }}
      onBlurCapture={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) { setFocused(false); pointerFocus.current = false } }}>
      <div className="hero-backdrop" aria-hidden="true">
        <HeroBackdrop src={media.banner ?? media.cover} title={media.title} reduced={reduced} />
        <DiscoverTrailer key={`trailer-${media.id}`} videoId={trailerId} title={media.title} active={trailerActive} />
        <div className="hero-scrim" />
      </div>
      <div className="hero-inner">
        {/* 自动切换时不念（每 8 秒打断一次读屏）；停住之后换的那张才念 */}
        <div aria-live={running ? 'off' : 'polite'}>
          <div className="hero-slide" key={media.id} role="group" aria-roledescription="幻灯片" aria-label={`第 ${index + 1} 部，共 ${count} 部`}>
            <MediaEntryLink id={media.id} className="hero-art" tabIndex={-1} aria-hidden="true">
              <MediaArtwork src={media.cover} title={media.title} eager />
            </MediaEntryLink>
            <div className="hero-text">
              <p className="hero-kicker">{label}</p>
              <h2 className="hero-title"><MediaEntryLink id={media.id}>{media.title}</MediaEntryLink></h2>
              {media.titleNative && <p className="hero-native" lang="ja">{media.titleNative}</p>}
              <p className="hero-meta">
                {meta.score && <span className="hero-score"><Icon name="star" size={14} /><span className="visually-hidden">评分 </span>{meta.score}</span>}
                {meta.parts.map((part, i) => <span key={`${i}-${part}`}>{part}</span>)}
              </p>
              {media.description && <p className="hero-desc">{media.description}</p>}
              <div className="hero-actions">
                <MediaPreview media={media} className="btn btn--primary" onOpenChange={setDialogOpen}>预览</MediaPreview>
                <MediaEntryLink id={media.id} className="btn hero-open">作品页</MediaEntryLink>
              </div>
            </div>
          </div>
        </div>
        {count > 1 && (
          <div className="hero-controls">
            <div className="hero-dots" role="group" aria-label="切换精选作品">
              {items.map((item, i) => (
                <button key={item.id} type="button" className="hero-dot" aria-label={`第 ${i + 1} 部：${item.title}`}
                  aria-current={i === index ? 'true' : undefined} onClick={() => select(i)}>
                  <span />
                </button>
              ))}
            </div>
            {!reduced && (
              <button type="button" className="hero-pause" aria-label={userPaused ? '继续自动切换' : '暂停自动切换'} onClick={() => setUserPaused((paused) => !paused)}>
                <Icon name={userPaused ? 'play' : 'pause'} size={14} />
              </button>
            )}
          </div>
        )}
      </div>
    </section>
  )
}
