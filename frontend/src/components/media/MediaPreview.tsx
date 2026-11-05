import { useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Icon } from '../ui/Icon'
import { MediaArtwork } from './MediaArtwork'
import { MediaPlayButton } from './MediaPlayButton'
import { MediaTorrentButton } from './MediaTorrentButton'
import type { MediaSummary } from './types'
import { MediaDetails, MediaRelations, MediaExternalLinks } from './MediaDetails'
import { useMediaDetails } from './useMediaDetails'
import './media-preview.css'

/**
 * 作品预览窗口（详情 + 播放 / 磁力 / 预告片）；播放仍由用户明确关联实际本地文件。
 *
 * 触发按钮默认念「预览 <作品名>」。按钮里已经有可见的文字（作品名、集号）时传 label={null}，
 * 名字就取可见文字 —— 否则 aria-label 盖过可见文字，语音控制的用户念屏幕上的字点不到它（WCAG 2.5.3）。
 */
export function MediaPreview({ media: initialMedia, children, className = 'btn', onOpenChange, openSignal = 0, label }: { media: MediaSummary; children: ReactNode; className?: string; openSignal?: number; onOpenChange?: (open: boolean) => void; label?: string | null }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const titleId = useId()
  const [open, setOpen] = useState(false)
  const detail = useMediaDetails(initialMedia.id, open)
  const media = detail.media ?? initialMedia
  useEffect(() => { if (openSignal > 0) trigger.current?.click() }, [openSignal])
  // 开 / 关都从 open 这一处报给调用方：开着的时候整个组件被卸掉（后台刷新换掉了它所在的那张卡），
  // 清理函数照样报一次「关了」—— 否则调用方（横幅的暂停、卡片的浮层）会一直以为对话框开着
  const notify = useRef(onOpenChange)
  useEffect(() => { notify.current = onOpenChange })
  useEffect(() => {
    if (!open) return
    notify.current?.(true)
    return () => notify.current?.(false)
  }, [open])
  return <>
    <button type="button" className={className} ref={trigger} onClick={() => { setOpen(true); dialog.current?.showModal() }} aria-label={label === undefined ? `预览 ${media.title}` : label ?? undefined}>{children}</button>
    <dialog ref={dialog} className="media-preview" aria-labelledby={titleId} onClose={event => {
      // 内层预告片或本地选集关闭时，外层预览仍保持打开。
      if (event.target !== dialog.current) return
      setOpen(false); trigger.current?.focus()
    }}
      onClick={(event) => { if (event.target === dialog.current) dialog.current.close() }}>
      {open && <article className="media-preview-body">
        <div className="media-preview-backdrop" aria-hidden="true"><MediaArtwork src={media.banner ?? media.cover} title={media.title} /></div>
        <button type="button" className="icon-button media-preview-close" aria-label="关闭预览" autoFocus onClick={() => dialog.current?.close()}><Icon name="close" size={20} /></button>
        <MediaDetails media={media} titleId={titleId} />
        <div className="media-preview-actions">
          <MediaExternalLinks media={media} preview />
          <MediaPlayButton media={media} onOpenChange={() => {}} />
          <MediaTorrentButton media={media} onOpenChange={() => {}} />
          {media.trailerId && /^[\w-]{11}$/.test(media.trailerId) && <TrailerPreview media={media} />}
        </div>
        {detail.loading && <p className="result" role="status">正在读取作品详情…</p>}
        {detail.error && <p className="result result--err" role="alert">{detail.error} <button className="link" onClick={detail.retry}>重试</button></p>}
        <MediaRelations media={media} preview />
      </article>}
    </dialog>
  </>
}

export function TrailerPreview({ media, className = 'media-preview-link' }: { media: MediaSummary; className?: string }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  return <>
    <button type="button" className={className} ref={trigger} onClick={() => { setOpen(true); dialog.current?.showModal() }}>预告片</button>
    <dialog ref={dialog} className="media-trailer-dialog" aria-label={`${media.title} 预告片`}
      onClose={event => { event.stopPropagation(); setOpen(false); trigger.current?.focus() }}
      onClick={event => { if (event.target === dialog.current) dialog.current.close() }}>
      <button className="icon-button media-preview-close" type="button" autoFocus aria-label="关闭预告片" onClick={() => dialog.current?.close()}><Icon name="close" size={20} /></button>
      {open && <iframe title={`${media.title} 预告片`} src={`https://www.youtube-nocookie.com/embed/${media.trailerId}?autoplay=1&playsinline=1&rel=0`}
        allow="autoplay; encrypted-media; picture-in-picture; fullscreen" allowFullScreen referrerPolicy="origin" />}
    </dialog>
  </>
}
