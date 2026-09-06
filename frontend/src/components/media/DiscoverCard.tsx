import { airingDistance, audienceColor } from './media-format'
import { createPortal } from 'react-dom'
import { useEffect, useRef, useState } from 'react'
import { MediaEntryLink } from './MediaEntryLink'
import { MediaListEditor } from './MediaListEditor'
import { useCollection } from './CollectionContext'
import { COLLECTION_LABELS, entryStatus } from '../../lib/media'
import { Icon } from '../ui/Icon'
import { MediaArtwork } from './MediaArtwork'
import { MediaPreview } from './MediaPreview'
import { MediaPlayButton } from './MediaPlayButton'
import { MediaTorrentButton } from './MediaTorrentButton'
import { useMediaTrailer } from './useMediaTrailer'
import { DiscoverTrailer } from './DiscoverTrailer'
import { useDesktopLayout } from './useDesktopLayout'
import { useReducedMotionPreference } from './useReducedMotionPreference'
import type { MediaSummary } from './types'

/** 原版悬停层覆盖整张卡片，向上扩展 5%、左右各扩展 1.5%。 */
export function DiscoverCard({ media: initialMedia, showTrailer = true, badge }: { media: MediaSummary; showTrailer?: boolean; badge?: string }) {
  const collection = useCollection()
  const entry = collection.entries.find(item => item.anilistId === initialMedia.id)
  const media = { ...initialMedia, watched: entry?.currentEpisode ?? initialMedia.watched }
  const [menu, setMenu] = useState<{ left: number; top: number }>()
  const [previewSignal, setPreviewSignal] = useState(0)
  const menuRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!menu) return
    const close = (event: PointerEvent) => { if (!menuRef.current?.contains(event.target as Node)) setMenu(undefined) }
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') setMenu(undefined) }
    document.addEventListener('pointerdown', close); document.addEventListener('keydown', key)
    return () => { document.removeEventListener('pointerdown', close); document.removeEventListener('keydown', key) }
  }, [menu])
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)
  const [renderPopup, setRenderPopup] = useState(false)
  const closeTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const pointerFocus = useRef(false)
  const desktop = useDesktopLayout()
  const reduced = useReducedMotionPreference()
  const open = desktop && (hovered || focused || previewOpen)
  const trailerId = useMediaTrailer(media, showTrailer && open && !previewOpen && !reduced)
  const subtitle = media.titleEnglish?.toLowerCase() !== media.title.toLowerCase() ? media.titleEnglish : undefined
  const progress = media.episodes && media.watched ? Math.min(100, media.watched / media.episodes * 100) : 0
  const completed = media.episodes !== null && media.watched >= media.episodes
  useEffect(() => {
    clearTimeout(closeTimer.current)
    if (open) setRenderPopup(true)
    else closeTimer.current = setTimeout(() => setRenderPopup(false), 35)
    return () => clearTimeout(closeTimer.current)
  }, [open])
  return <li className="poster discover-card" data-hovered={open || undefined}
    onContextMenu={event => { if ((event.target as Element).closest('dialog')) return; event.preventDefault(); setMenu({ left: Math.min(event.clientX, window.innerWidth - 216), top: Math.min(event.clientY, window.innerHeight - 112) }) }}
    onPointerEnter={event => { if (event.pointerType !== 'touch') setHovered(true) }} onPointerLeave={() => setHovered(false)}
    onPointerDownCapture={() => { pointerFocus.current = true; setFocused(false) }}
    onKeyDownCapture={() => { pointerFocus.current = false; setFocused(true) }}
    onFocusCapture={() => { if (!pointerFocus.current) setFocused(true) }}
    onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget)) { setFocused(false); pointerFocus.current = false } }}>
    <MediaEntryLink id={media.id} className="discover-card-cover" aria-label={`打开作品 ${media.title}`}>
      <MediaArtwork src={media.cover} title={media.title} reveal />
      {progress > 0 && !completed && <span className="discover-card-progress"><span style={{ width: `${progress}%` }} /></span>}
      {media.watched > 0 && <span className="discover-card-count">{media.watched}{media.episodes !== null && ` / ${media.episodes}`}</span>}
      {badge && <span className="media-relation-badge">{badge}</span>}
      {media.recentAiring ? <span className="discover-recent"><strong>{media.recentAiring.episode}<small>/{media.episodes ?? '—'}</small></strong><time dateTime={new Date(media.recentAiring.at * 1000).toISOString()}>{airingDistance(media.recentAiring.at)}</time></span> : (media.status === 'RELEASING' || media.status === 'NOT_YET_RELEASED') && <span className="discover-airing" data-status={media.status} aria-label={media.status === 'RELEASING' ? '放送中' : '尚未播出'}><Icon name="broadcast" size={16} /></span>}
      {!!entry?.score && <span className="discover-personal-score"><Icon name="star" size={14} />{entry.score}</span>}
    </MediaEntryLink>
    <p className="discover-card-title"><MediaEntryLink id={media.id}>{media.title}</MediaEntryLink></p>
    {(media.season || media.year) && <p className="discover-card-season">{media.season} {media.year}</p>}
    <div className="discover-card-popup" data-state={open ? 'open' : 'closed'} aria-hidden={!open} inert={!open}>
      {renderPopup && <div className="discover-card-popup-content">
        <div>
          <MediaEntryLink id={media.id} className="discover-card-banner" aria-label={`打开作品 ${media.title}`}>
            <MediaArtwork src={media.banner ?? media.cover} title={media.title} />
            <DiscoverTrailer videoId={trailerId} title={media.title} active={open && hovered && showTrailer && !reduced && !previewOpen} variant="card" />
            <span className="discover-card-banner-gradient" />
            {progress > 0 && !completed && <span className="discover-card-progress"><span style={{ width: `${progress}%` }} /></span>}
            {media.watched > 0 && <span className="discover-card-count">{media.watched} / {media.episodes ?? '—'}</span>}
            {(media.status === 'RELEASING' || media.status === 'NOT_YET_RELEASED') && <span className="discover-airing" data-status={media.status} aria-label={media.status === 'RELEASING' ? '放送中' : '尚未播出'}><Icon name="broadcast" size={16} /></span>}
          </MediaEntryLink>
          <h3 className="discover-card-popup-title"><MediaEntryLink id={media.id} className="discover-card-title-link">{media.title}</MediaEntryLink></h3>
          {subtitle && <p className="discover-card-native">{subtitle}</p>}
          <p className="discover-card-year"><Icon name="calendar" size={14} />{media.season} {media.year}{media.format && media.format !== 'TV' ? ` - ${media.format}` : ''}</p>
          <div className="discover-card-play-row">
            <MediaPlayButton media={media} onOpenChange={setPreviewOpen} />
            <MediaTorrentButton media={media} onOpenChange={setPreviewOpen} />
          </div>
          {media.nextAiring && <p className="discover-card-next">第 {media.nextAiring.episode} 集 {airingDistance(media.nextAiring.at)}播出</p>}
          {entry && entryStatus(entry) !== 'watching' && <p className="discover-card-status">{COLLECTION_LABELS[entryStatus(entry)]}</p>}
        </div>
        <footer className="discover-card-footer">
          <MediaListEditor media={media} onOpenChange={setPreviewOpen} />
          {!!media.score && <span className="discover-card-score" style={{ color: audienceColor(media.score) }} data-score={media.score >= 82 ? 'high' : 'normal'}><Icon name="heart" size={12} />{media.score / 10}</span>}
        </footer>
      </div>}
    </div>
    <MediaPreview media={media} className="visually-hidden" openSignal={previewSignal} onOpenChange={setPreviewOpen}>预览</MediaPreview>
    {menu && createPortal(<div ref={menuRef} className="media-context-menu" role="menu" aria-label={`${media.title} 操作`} style={menu}>
      <MediaEntryLink id={media.id} role="menuitem">打开作品页</MediaEntryLink>
      <button type="button" role="menuitem" autoFocus onClick={() => { setMenu(undefined); setPreviewSignal(value => value + 1) }}><Icon name="info" size={18} />预览</button>
    </div>, document.body)}
  </li>
}
