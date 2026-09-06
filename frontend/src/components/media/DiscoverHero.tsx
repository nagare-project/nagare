import { audienceColor } from './media-format'
import { useRef, useState } from 'react'
import { AnimatePresence, domAnimation, LazyMotion, m } from 'motion/react'
import { Icon } from '../ui/Icon'
import { MediaArtwork } from './MediaArtwork'
import { MediaEntryLink } from './MediaEntryLink'
import { MediaPreview } from './MediaPreview'
import { DiscoverTrailer } from './DiscoverTrailer'
import { useDiscoverCarousel } from './useDiscoverCarousel'
import { useReducedMotionPreference } from './useReducedMotionPreference'
import { useDesktopLayout } from './useDesktopLayout'
import { useMediaTrailer } from './useMediaTrailer'
import type { MediaSummary } from './types'

/** 参照 seanime：12 秒轮播、900ms 退场、封面缩放与分层文字入场。 */
export function DiscoverHero({ items, showMetadata = true }: { items: MediaSummary[]; showMetadata?: boolean }) {
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const [paused, setPaused] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)
  const pointerFocus = useRef(false)
  const desktop = useDesktopLayout()
  const reduced = useReducedMotionPreference()
  const { activeIndex, selectedIndex, transitioning, select } = useDiscoverCarousel(Math.min(items.length, 12), paused || hovered || focused || previewOpen || !desktop || !showMetadata, reduced)

  const trailerId = useMediaTrailer(items[activeIndex], desktop && showMetadata && hovered && !previewOpen && !transitioning && !reduced)
  if (!items.length) return null
  const media = items[activeIndex]!
  return (
    <LazyMotion features={domAnimation}><m.section className={transitioning ? 'hero hero--transitioning' : 'hero'} aria-label="精选作品" aria-roledescription="轮播"
      initial={reduced ? false : { opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 1.2 }}
      onPointerDownCapture={() => { pointerFocus.current = true; setFocused(false) }}
      onKeyDownCapture={() => { pointerFocus.current = false; setFocused(true) }}
      onFocusCapture={() => { if (!pointerFocus.current) setFocused(true) }}
      onBlurCapture={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) { setFocused(false); pointerFocus.current = false } }}>
      <div className="hero-bg" aria-hidden="true">
        <MediaArtwork src={media.banner ?? media.cover} title={media.title} eager />
        <DiscoverTrailer key={media.id} videoId={trailerId} title={media.title}
          active={desktop && showMetadata && hovered && !previewOpen && !transitioning && !reduced} />
        <div className="hero-transition-shade" />
        <div className="hero-gradient-top" /><div className="hero-gradient-left" /><div className="hero-gradient-rail" /><div className="hero-gradient-bottom" />
      </div>
      {showMetadata && desktop && items.length > 1 && <div className="hero-dots" aria-label="切换精选作品">
        {items.slice(0, 12).map((m, i) => <button key={m.id} type="button" aria-pressed={i === selectedIndex} aria-label={m.title}
          className={i === selectedIndex ? 'hero-dot hero-dot--on' : 'hero-dot'} onClick={() => select(i)} />)}
        <button type="button" className="hero-pause" aria-label={paused ? '自动轮播' : '暂停轮播'} aria-pressed={paused} onClick={() => setPaused(!paused)}><Icon name={paused ? 'play' : 'pause'} size={14} /></button>
      </div>}
      <div className="hero-stage" aria-busy={transitioning}>
        <AnimatePresence>
          {desktop && showMetadata && !transitioning && <m.div key={media.id} className="hero-body"
            onPointerEnter={event => { if (event.pointerType !== 'touch') setHovered(true) }}
            onPointerLeave={() => setHovered(false)}
            initial={reduced ? false : { opacity: 0, x: -40 }} animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: reduced ? 0 : -20 }}
            transition={reduced ? { duration: 0 } : { type: 'spring', damping: 20, stiffness: 100 }}>
            <m.span className="hero-art" initial={reduced ? false : { opacity: 0, scale: .7, skewX: 5, skewY: 5 }}
              animate={{ opacity: 1, scale: 1, skewX: 0, skewY: 0 }} exit={{ opacity: 1, scale: 1, skewY: 1 }} transition={{ duration: reduced ? 0 : .5 }}>
              <MediaEntryLink id={media.id} aria-label={`打开作品 ${media.title}`}><MediaArtwork src={media.cover} title={media.title} eager /></MediaEntryLink>
            </m.span>
            <m.div className="hero-text" initial={reduced ? false : { opacity: 0, x: 10 }}
              animate={{ opacity: 1, x: 0 }} transition={{ duration: reduced ? 0 : .5, delay: reduced ? 0 : .6 }}>
              <h2 className="hero-title" aria-label={media.title}>
                <MediaEntryLink id={media.id}>{media.title.split(' ').map((word, i) => <m.span key={i} aria-hidden="true"
                  initial={reduced ? false : { opacity: 0 }} animate={{ opacity: 1 }}
                  transition={{ duration: reduced ? 0 : 2, delay: reduced ? 0 : i * .2 }}>{word}{i < media.title.split(' ').length - 1 ? ' ' : ''}</m.span>)}</MediaEntryLink>
              </h2>
              <p className="hero-genres">{media.genres.slice(0, 3).map((genre) => <span key={genre}>{genre}</span>)}</p>
              <p className="hero-meta">
                {!!media.score && <span className="hero-score discover-card-score" style={{ color: audienceColor(media.score) }}><Icon name="heart" size={12} />{media.score / 10}</span>}
                {!!media.nextAiring && <span className="hero-releasing"><Icon name="broadcast" size={18} />放送中</span>}
                {media.format !== 'MOVIE' && (media.nextAiring?.episode || media.episodes) && <span>{media.nextAiring ? `${Math.max(0, media.nextAiring.episode - 1)} 集已播出` : `共 ${media.episodes} 集`}</span>}
              </p>
              <m.div initial={reduced ? false : { opacity: 0, x: 10 }} animate={{ opacity: 1, x: 0 }}
                transition={{ duration: reduced ? 0 : .5, delay: reduced ? 0 : .7 }}>
                {media.description && <p className="hero-desc">{media.description}</p>}
                <MediaPreview media={media} className="btn hero-preview" onOpenChange={setPreviewOpen}>预览</MediaPreview>
              </m.div>
            </m.div>
          </m.div>}
        </AnimatePresence>
      </div>
    </m.section></LazyMotion>
  )
}
