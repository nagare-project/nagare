import { useRef, useState } from 'react'
import { COLLECTION_LABELS, entryStatus } from '../../lib/catalog'
import type { CollectionEdit, CollectionStatus } from '../../lib/catalog'
import { errorText } from '../../lib/format'
import { Icon } from '../ui/Icon'
import { useCollection } from './CollectionContext'
import type { MediaSummary } from './types'
import './media-list-editor.css'

/** 收藏按账号服务保存；扩展字段保存在该账号的本机备注中。 */
export function MediaListEditor({ media, onOpenChange = () => {} }: { media: MediaSummary; onOpenChange?: (open: boolean) => void }) {
  const collection = useCollection()
  const entry = collection.entries.find(e => e.anilistId === media.id)
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const [draft, setDraft] = useState<CollectionEdit>({ status: 'plan_to_watch', progress: 0, score: null, startedAt: '', completedAt: '', repeat: 0 })
  const [pending, setPending] = useState(false)
  const busy = useRef(false)
  const [error, setError] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [position, setPosition] = useState<{ left: number; top: number }>()
  if (!collection.loggedIn) return null
  function open() {
    setDraft({ status: entry ? entryStatus(entry) : 'plan_to_watch', progress: entry?.currentEpisode ?? 0, score: entry?.score ?? null,
      startedAt: entry?.startedAt ?? '', completedAt: entry?.completedAt ?? '', repeat: entry?.repeat ?? 0 })
    setError(null); setConfirmDelete(false)
    // 原版桌面为贴近触发器的浮层，窄屏为居中窗口；始终限制在视口内。
    if (window.innerWidth > 1024) { const rect = trigger.current!.getBoundingClientRect(); setPosition({ left: Math.max(16, Math.min(rect.left, window.innerWidth - 656)), top: Math.max(16, Math.min(rect.bottom + 8, window.innerHeight - 370)) }) }
    else setPosition(undefined)
    dialog.current?.showModal(); onOpenChange(true)
  }
  async function submit(remove = false) {
    if (busy.current) return
    busy.current = true; setPending(true); setError(null)
    try { if (remove) await collection.remove(media.id); else await collection.save(media.id, draft); dialog.current?.close() }
    catch (err) { setError(errorText(err, '保存收藏失败，请重试')) }
    finally { busy.current = false; setPending(false) }
  }
  return <>
    <button ref={trigger} type="button" className="icon-button media-list-trigger" aria-label={`${entry ? '编辑收藏' : '加入列表'} ${media.title}`} onClick={open}><Icon name={entry ? 'edit' : 'plus'} size={18} /></button>
    <dialog ref={dialog} className="media-list-editor" style={position ? { ...position, right: 'auto', bottom: 'auto', margin: 0 } : undefined}
      aria-label={`编辑收藏 ${media.title}`} onCancel={event => { if (pending) event.preventDefault() }}
      onClose={event => { event.stopPropagation(); trigger.current?.focus(); onOpenChange(false) }}
      onClick={event => { if (event.target === dialog.current && !pending) dialog.current.close() }}>
      <form onSubmit={event => { event.preventDefault(); void submit() }}>
        <h2>{media.title}</h2>
        <button type="button" className="icon-button media-list-close" aria-label="关闭收藏编辑" disabled={pending} onClick={() => dialog.current?.close()}><Icon name="close" size={18} /></button>
        <div className="media-list-fields">
          <label>观看状态<select aria-label="观看状态" value={draft.status} disabled={pending} onChange={event => { const status = event.target.value as CollectionStatus; setDraft({ ...draft, status, progress: status === 'completed' && media.episodes ? media.episodes : draft.progress }) }}>
            {(Object.keys(COLLECTION_LABELS) as CollectionStatus[]).map(status => <option key={status} value={status}>{COLLECTION_LABELS[status]}</option>)}
          </select></label>
          <label>评分<input type="number" min={1} max={10} step={1} value={draft.score ?? ''} placeholder="未评分" disabled={pending} onChange={event => setDraft({ ...draft, score: event.target.value === '' ? null : Number(event.target.value) })} /></label>
          <label>观看进度<input type="number" min={0} max={media.episodes ?? 5000} step={1} value={draft.progress} required disabled={pending} onChange={event => setDraft({ ...draft, progress: Number(event.target.value) })} /></label>
          <label>开始日期<input type="date" value={draft.startedAt} disabled={pending} onChange={event => setDraft({ ...draft, startedAt: event.target.value })} /></label>
          <label>完成日期<input type="date" value={draft.completedAt} disabled={pending} onChange={event => setDraft({ ...draft, completedAt: event.target.value })} /></label>
          <label>重看次数<input type="number" min={0} max={1000} step={1} value={draft.repeat} disabled={pending} onChange={event => setDraft({ ...draft, repeat: Number(event.target.value) })} /></label>
        </div>
        <p className="media-list-note">状态、评分和进度同步账号；日期、重看次数与搁置标记保存在本机。</p>
        {error && <p className="result result--err" role="alert">{error}</p>}
        <footer>{entry && <div className="media-list-delete"><button type="button" className="icon-button" aria-label="删除收藏" disabled={pending} onClick={() => setConfirmDelete(!confirmDelete)}><Icon name="trash" size={20} /></button>
          {confirmDelete && <button type="button" className="btn btn--danger" disabled={pending} onClick={() => void submit(true)}>确认删除</button>}</div>}
          <button type="submit" className="btn btn--primary" disabled={pending}>{pending ? '正在保存…' : '保存'}</button></footer>
      </form>
    </dialog>
  </>
}
