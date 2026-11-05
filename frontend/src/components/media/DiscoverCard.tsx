import { createPortal } from 'react-dom'
import { useEffect, useRef, useState } from 'react'
import { COLLECTION_LABELS, entryStatus } from '../../lib/media'
import { posterToneStyle } from '../../lib/posterTones'
import { Icon } from '../ui/Icon'
import { useCollection } from './CollectionContext'
import { DiscoverTrailer } from './DiscoverTrailer'
import { airingDistance } from './media-format'
import { MediaArtwork } from './MediaArtwork'
import { MediaEntryLink } from './MediaEntryLink'
import { MediaListEditor } from './MediaListEditor'
import { MediaPlayButton } from './MediaPlayButton'
import { MediaPreview } from './MediaPreview'
import { MediaTorrentButton } from './MediaTorrentButton'
import { useMediaTrailer } from './useMediaTrailer'
import { useReducedMotionPreference } from './useReducedMotionPreference'
import { cardMeta, cardStatus, usePopupLayout } from './posterCardParts'
import type { MediaSummary } from './types'
import './poster-card.css'

/** 鼠标停在卡片上这么久才弹出浮层：扫过一排卡片时只看到抬起，不会一张张闪出来 */
export const CARD_HOVER_INTENT_MS = 300
/** 移开后浮层内容延迟卸载：快速移出再移入时不重建（预告片不会重新加载） */
const POPUP_UNMOUNT_MS = 35

/**
 * 作品海报卡（发现 / 季度 / 我的追番 / 搜索结果 / 作品页的关联与推荐共用）。DESIGN.md「卡片 · 海报卡」：
 * 3:4 封面 + 两行标题 +「2026 秋 · 12 集 · 已看 7」，整张卡是一个链接（点哪都进作品页）。
 * 宽屏用鼠标停一会儿，浮出一张浮层卡：静音预告片、播放、磁力、加入追番。
 * 海报色写在卡片根元素自己的行内样式里（lib/posterTones.ts），只用于悬停边框与浮层里的评分。
 */
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
  const [intent, setIntent] = useState(false)
  const [focused, setFocused] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)
  const [renderPopup, setRenderPopup] = useState(false)
  // 按 Esc 收起了浮层（WCAG 1.4.13：悬停 / 聚焦浮出的内容要能不挪鼠标、不移焦点就收起）。
  // 直到鼠标移开、焦点离开这张卡才算结束：再停回来、再 Tab 进来是新的一次，照常弹出
  const [dismissed, setDismissed] = useState(false)
  if (dismissed && !hovered && !focused) setDismissed(false)
  const closeTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const pointerFocus = useRef(false)
  const linkRef = useRef<HTMLAnchorElement>(null)
  const popupRef = useRef<HTMLDivElement>(null)
  const popupLayout = usePopupLayout()
  const reduced = useReducedMotionPreference()
  const open = popupLayout && !dismissed && (intent || focused || previewOpen)
  const trailerId = useMediaTrailer(media, showTrailer && open && !previewOpen && !reduced)
  const progress = media.episodes && media.watched ? Math.min(100, media.watched / media.episodes * 100) : 0
  const completed = media.episodes !== null && media.watched >= media.episodes
  const status = cardStatus(media)
  const meta = cardMeta(media)
  useEffect(() => {
    if (!hovered) return
    const timer = setTimeout(() => setIntent(true), CARD_HOVER_INTENT_MS)
    return () => clearTimeout(timer)
  }, [hovered])
  // 浮层开着、且里面没有开着的对话框（对话框自己处理 Esc）时，Esc 收起浮层。
  // 挂在 document 上：鼠标悬停弹出时焦点往往不在这张卡里
  useEffect(() => {
    if (!open || previewOpen) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      // 焦点在浮层里：浮层收起后是 inert 的，先把焦点交还给卡片链接
      if (popupRef.current?.contains(document.activeElement)) linkRef.current?.focus()
      setDismissed(true)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, previewOpen])
  useEffect(() => {
    clearTimeout(closeTimer.current)
    if (open) setRenderPopup(true)
    else closeTimer.current = setTimeout(() => setRenderPopup(false), POPUP_UNMOUNT_MS)
    return () => clearTimeout(closeTimer.current)
  }, [open])

  return <li className="poster-card" data-open={open || undefined} style={posterToneStyle(media.posterTones)}
    onContextMenu={event => { if ((event.target as Element).closest('dialog')) return; event.preventDefault(); setMenu({ left: Math.min(event.clientX, window.innerWidth - 216), top: Math.min(event.clientY, window.innerHeight - 112) }) }}
    onPointerEnter={event => {
      if (event.pointerType === 'touch') return
      setHovered(true)
      // 刚移开又移回来（浮层还没卸载）：直接接着开，不重新等，也不重建浮层里的预告片
      if (renderPopup) setIntent(true)
    }}
    onPointerLeave={() => { setHovered(false); setIntent(false) }}
    onPointerDownCapture={() => { pointerFocus.current = true; setFocused(false) }}
    onKeyDownCapture={() => { pointerFocus.current = false; setFocused(true) }}
    onFocusCapture={() => { if (!pointerFocus.current) setFocused(true) }}
    onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget)) { setFocused(false); pointerFocus.current = false } }}>
    <MediaEntryLink ref={linkRef} id={media.id} className="poster-card-link">
      {/* 封面上的角标只是视觉；同样的信息以文字放在链接里（下面的 visually-hidden），读屏不会漏 */}
      <span className="poster-card-cover" aria-hidden="true">
        <MediaArtwork src={media.cover} title={media.title} reveal />
        {badge && <span className="poster-card-badge poster-card-badge--relation">{badge}</span>}
        {status && <span className="poster-card-badge poster-card-badge--status" data-tone={status.tone}>{status.short}</span>}
        {!!entry?.score && <span className="poster-card-badge poster-card-badge--score"><Icon name="star" size={12} />{entry.score}</span>}
        {progress > 0 && !completed && <span className="poster-card-progress"><span style={{ width: `${progress}%` }} /></span>}
      </span>
      <span className="poster-card-body">
        <span className="poster-card-title">{media.title}</span>
        {meta && <span className="poster-card-meta">{meta}</span>}
      </span>
      {(badge || status || entry?.score) && <span className="visually-hidden">{[badge, status?.long, entry?.score ? `我的评分 ${entry.score}` : ''].filter(Boolean).join('，')}</span>}
    </MediaEntryLink>
    <div ref={popupRef} className="poster-card-popup" data-state={open ? 'open' : 'closed'} aria-hidden={!open} inert={!open}>
      {renderPopup && <div className="poster-card-popup-content">
        <MediaEntryLink id={media.id} className="poster-card-popup-banner" tabIndex={-1} aria-hidden="true">
          <MediaArtwork src={media.banner ?? media.cover} title={media.title} />
          <DiscoverTrailer videoId={trailerId} title={media.title} active={open && hovered && showTrailer && !reduced && !previewOpen} variant="card" />
        </MediaEntryLink>
        <h3 className="poster-card-popup-title"><MediaEntryLink id={media.id}>{media.title}</MediaEntryLink></h3>
        <p className="poster-card-popup-meta">{[meta, media.nextAiring ? `第 ${media.nextAiring.episode} 集 ${airingDistance(media.nextAiring.at)}播出` : ''].filter(Boolean).join(' · ')}</p>
        <div className="poster-card-popup-actions">
          <MediaPlayButton media={media} onOpenChange={setPreviewOpen} />
          <MediaTorrentButton media={media} onOpenChange={setPreviewOpen} />
        </div>
        <footer className="poster-card-popup-footer">
          <MediaListEditor media={media} onOpenChange={setPreviewOpen} />
          {entry && entryStatus(entry) !== 'watching' && <span className="poster-card-popup-status">{COLLECTION_LABELS[entryStatus(entry)]}</span>}
          {!!media.score && <span className="poster-card-score"><Icon name="star" size={12} /><span className="visually-hidden">评分 </span>{(media.score / 10).toFixed(1)}</span>}
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
