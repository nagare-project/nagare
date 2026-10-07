import { useEffect, useId, useRef, useState } from 'react'
import { dismissSyncedElsewhere, formatEpisodeList, needsConfirmation, setAssociation } from '../../lib/associations'
import type { AssociationChange, SyncedRecord } from '../../lib/associations'
import type { LibraryCluster } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { Icon } from '../ui/Icon'
import { AssociationDialog } from './AssociationDialog'
import './association.css'

/**
 * 作品页上的「对应作品」：这个分组记在哪部目录作品上，可以确认、更换、标为不是目录作品、
 * 回到自动匹配；之前写到别的作品上的集也在这里列出（nagare 不替用户撤销）。
 *
 * 所有改动（包括从选作品对话框里保存的）都走同一条路：保存 → 重读媒体库，期间按钮全部禁用，
 * 免得重读完成前点到按旧数据画出来的按钮，把刚做的选择又盖掉。
 */
export function AssociationPanel({ cluster, onChanged }: { cluster: LibraryCluster; onChanged: () => Promise<void> | void }) {
  const [picking, setPicking] = useState(false)
  const [confirmingAuto, setConfirmingAuto] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const busy = useRef(false)
  const panel = useRef<HTMLElement>(null)
  const status = useRef<HTMLParagraphElement>(null)
  const errorLine = useRef<HTMLParagraphElement>(null)
  const pickButton = useRef<HTMLButtonElement>(null)
  const autoButton = useRef<HTMLButtonElement>(null)
  // 操作之后焦点该去哪：点过的按钮在保存期间被禁用、成功后多半还会消失，焦点会掉到页面顶部。
  // 等 React 把新内容提交之后再移（否则读屏念的是旧句子），而且只在焦点还在面板里或掉到了
  // body 时才移 —— 用户在慢的重读期间已经 Tab 到别处，不该被拽回来。
  const focusNext = useRef<'status' | 'error' | 'pick' | 'auto' | null>(null)
  const { association, matched } = cluster
  const synced = association?.syncedElsewhere ?? []

  useEffect(() => {
    const target = focusNext.current
    if (target === null || pending) return
    focusNext.current = null
    const active = document.activeElement
    if (active !== null && active !== document.body && !panel.current?.contains(active)) return
    ;({ status, error: errorLine, pick: pickButton, auto: autoButton })[target].current?.focus()
  })

  /** 保存并重读媒体库；失败时抛出，由调用方决定在哪里显示 */
  async function apply(action: () => Promise<unknown>): Promise<void> {
    if (busy.current) throw new Error('上一次修改还在保存，请稍候')
    busy.current = true
    setPending(true)
    try {
      await action()
      setConfirmingAuto(false)
      setError(null)
      await onChanged()
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  /** 面板上的按钮：成功后焦点放到（已经更新了的）状态行，失败时放到错误说明上 */
  async function run(action: () => Promise<unknown>): Promise<void> {
    setError(null)
    try {
      await apply(action)
      focusNext.current = 'status'
    } catch (err) {
      setError(errorText(err, '保存失败'))
      focusNext.current = 'error'
    }
  }

  function cancelConfirm(): void {
    setConfirmingAuto(false)
    focusNext.current = 'auto'
  }

  const change = (next: AssociationChange) => run(() => setAssociation(cluster.clusterKey, next))
  // 回到自动匹配会连同「写到别的作品」的清单一起删掉：清单不空时只打开确认，真正删除只在「确定」里
  const backToAuto = () => (synced.length > 0 ? setConfirmingAuto(true) : void change({ mode: 'auto' }))

  return (
    <section ref={panel} className="assoc-panel" aria-label="对应作品" aria-busy={pending}>
      <div className="assoc-panel-row">
        <Icon name={association?.mode === 'manual' ? 'check' : 'info'} size={18} />
        <p className="assoc-panel-status" ref={status} tabIndex={-1}>
          <StatusLine cluster={cluster} />
        </p>
        <div className="assoc-panel-actions">
          {association === undefined && matched && (
            <button
              type="button"
              className="btn btn--sm btn--primary"
              aria-label={`确认对应作品是「${matched.title || `作品 ${matched.anilistId}`}」`}
              disabled={pending}
              onClick={() => void change({ mode: 'manual', anilistId: matched.anilistId })}
            >
              确认
            </button>
          )}
          <button ref={pickButton} type="button" className="btn btn--sm" disabled={pending} onClick={() => setPicking(true)}>
            {association === undefined && !matched ? '选择作品' : '更换'}
          </button>
          {association !== undefined && (
            <button ref={autoButton} type="button" className="btn btn--sm" disabled={pending || confirmingAuto} onClick={backToAuto}>
              回到自动匹配
            </button>
          )}
        </div>
      </div>

      {pending && (
        <p className="result result--dim" role="status">
          正在保存 …
        </p>
      )}
      {confirmingAuto && <ConfirmAuto pending={pending} onConfirm={() => void change({ mode: 'auto' })} onCancel={cancelConfirm} />}
      {error && (
        <p ref={errorLine} className="result result--err" role="alert" tabIndex={-1}>
          {error}
        </p>
      )}
      {synced.map((record) => (
        <SyncedNotice key={record.anilistId} record={record} pending={pending} onDismiss={() => void run(() => dismissSyncedElsewhere(cluster.clusterKey, record.anilistId))} />
      ))}

      <AssociationDialog
        cluster={cluster}
        open={picking}
        onClose={() => {
          setPicking(false)
          // Safari 里鼠标点按钮不会聚焦它，原生 dialog 的焦点归还会落空：显式还给打开它的按钮
          // （保存还没结束时它是禁用的，等保存完再还）
          focusNext.current = 'pick'
        }}
        onChoose={(choice) => apply(() => setAssociation(cluster.clusterKey, choice))}
        onLateError={setError}
      />
    </section>
  )
}

/** 删除清单前的确认。出现时焦点放到「取消」上（不小心按两下回车也不会删），Esc 取消 */
function ConfirmAuto({ pending, onConfirm, onCancel }: { pending: boolean; onConfirm: () => void; onCancel: () => void }) {
  const cancel = useRef<HTMLButtonElement>(null)
  const textId = useId()
  useEffect(() => cancel.current?.focus(), [])
  return (
    <div
      className="assoc-confirm"
      role="group"
      aria-label="确认回到自动匹配"
      aria-describedby={textId}
      onKeyDown={(event) => {
        if (event.key === 'Escape') onCancel()
      }}
    >
      <p id={textId}>回到自动匹配后，下面「之前记到别的作品」的清单也会一起删掉。确定吗？</p>
      <button type="button" className="btn btn--sm btn--danger" disabled={pending} onClick={onConfirm}>
        确定删除并回到自动匹配
      </button>
      <button ref={cancel} type="button" className="btn btn--sm" disabled={pending} onClick={onCancel}>
        取消
      </button>
    </div>
  )
}

function StatusLine({ cluster }: { cluster: LibraryCluster }) {
  const { association, matched } = cluster
  if (association?.mode === 'manual' && association.anilistId) {
    return (
      <>
        对应作品：「<WorkLink id={association.anilistId} title={association.title} />」
      </>
    )
  }
  if (association?.mode === 'none') return <>已标为不是目录里的作品：不匹配弹幕，看完也不记录。</>
  if (matched) {
    return (
      <>
        自动匹配到「<WorkLink id={matched.anilistId} title={matched.title} />」，还没确认
        {needsConfirmation(cluster) && '（这个分组的标题可能没认准）'}。
      </>
    )
  }
  return <>还没有对应的作品。播放时会按文件名自动匹配，认错了弹幕和看完的集数都会记到别的作品上。</>
}

function WorkLink({ id, title }: { id: number; title?: string }) {
  return (
    <MediaEntryLink id={id} className="link">
      {title || `作品 ${id}`}
    </MediaEntryLink>
  )
}

function SyncedNotice({ record, pending, onDismiss }: { record: SyncedRecord; pending: boolean; onDismiss: () => void }) {
  const name = record.title || `作品 ${record.anilistId}`
  return (
    <div className="assoc-synced" role="note">
      <p>
        {formatEpisodeList(record.episodes)}之前记到了「{name}」上，nagare 不会替你撤销。
      </p>
      <MediaEntryLink id={record.anilistId} className="btn btn--sm">
        去「{name}」改进度
      </MediaEntryLink>
      <button type="button" className="btn btn--sm" aria-label={`「${name}」那边已处理，不再提示`} disabled={pending} onClick={onDismiss}>
        已处理
      </button>
    </div>
  )
}
