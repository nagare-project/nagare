import { useEffect, useRef, useState } from 'react'
import { clearCoverCache } from '../../lib/endpoints'
import type { CoverCacheClearResult, CoverCacheInfo } from '../../lib/endpoints'
import { errorText, formatBytes } from '../../lib/format'
import { mono } from '../../theme'
import './cards.css'

export interface CoverCacheCardProps {
  cache: CoverCacheInfo
  /** 清空之后刷新设置，占用改成后端的新数字 */
  onReload: () => Promise<void>
}

/** 清空流程：空闲 → 内联二次确认 → 请求中 → 完成 / 失败 */
type ClearState =
  | { phase: 'idle' }
  | { phase: 'confirming' }
  | { phase: 'busy' }
  | { phase: 'done'; result: CoverCacheClearResult }
  | { phase: 'error'; message: string }

/** 清空之后的一句话（占用以上面刷新后的列表为准）；有正在显示的封面没删掉时说一声 */
export function clearedText(result: CoverCacheClearResult): string {
  return result.skipped > 0 ? `已清空（${result.skipped} 张正在显示，这次没删，稍后可再清一次）` : '已清空'
}

/** 状态行的文字：请求中、完成、失败各一句；空闲与确认中为空 */
function statusText(state: ClearState): string {
  switch (state.phase) {
    case 'busy':
      return '正在清空 …'
    case 'done':
      return clearedText(state.result)
    case 'error':
      return `清空失败：${state.message}`
    default:
      return ''
  }
}

/**
 * 「封面缓存」卡：发现页与媒体库的海报图存在本机，这里看占用、手动清空。
 * 二次确认做成内联（与退出卡一致，不用 window.confirm）。
 */
export function CoverCacheCard({ cache, onReload }: CoverCacheCardProps) {
  const [state, setState] = useState<ClearState>({ phase: 'idle' })
  const card = useRef<HTMLElement>(null)
  const clearButton = useRef<HTMLButtonElement>(null)
  const cancelButton = useRef<HTMLButtonElement>(null)
  // 只在用户自己从确认区退出来时把焦点还给「清空」按钮；首次渲染不抢焦点
  const returnFocus = useRef(false)
  const confirming = state.phase === 'confirming' || state.phase === 'busy'

  useEffect(() => {
    if (state.phase === 'confirming') {
      cancelButton.current?.focus()
      return
    }
    if (state.phase === 'busy' || !returnFocus.current) return
    returnFocus.current = false
    // 请求期间用户可能已经把焦点挪到别处：只在焦点还在卡片里、或掉到了 body 上时才移
    const active = document.activeElement
    if (active === null || active === document.body || card.current?.contains(active)) {
      clearButton.current?.focus()
    }
  }, [state.phase])

  async function handleConfirm(): Promise<void> {
    setState({ phase: 'busy' })
    returnFocus.current = true
    let result: CoverCacheClearResult
    try {
      result = await clearCoverCache()
    } catch (err) {
      console.error('清空封面缓存失败', err)
      setState({ phase: 'error', message: errorText(err, '请稍后再试') })
      return
    }
    // 先刷新设置再报完成：上面的占用换成后端的新数字，状态行与列表不会一时对不上。
    // 刷新失败不算清空失败（清空已经做完了）
    try {
      await onReload()
    } catch (err) {
      console.error('清空封面缓存后刷新设置失败', err)
    }
    setState({ phase: 'done', result })
  }

  function handleCancel(): void {
    returnFocus.current = true
    setState({ phase: 'idle' })
  }

  return (
    <section ref={card} className="panel settings-card" aria-labelledby="cover-cache-heading">
      <h2 id="cover-cache-heading" className="panel-heading">
        封面缓存
      </h2>
      <dl className="kv-list">
        <dt>占用</dt>
        <dd style={mono}>
          {formatBytes(cache.bytes)} · {cache.count} 张
        </dd>
        <dt>上限</dt>
        <dd style={mono}>{formatBytes(cache.maxBytes)}</dd>
      </dl>
      <p className="page-notice-copy">
        发现页与媒体库的海报图存在本机。超过上限时自动删掉最久没看的，媒体库与离线目录用到的最后才删。清空之后海报会在下次显示时重新下载，断网时没有缓存的海报不显示。
      </p>

      {confirming ? (
        <div className="quit-confirm" role="group" aria-label="确认清空封面缓存">
          <p className="quit-confirm-copy">确定清空封面缓存吗？</p>
          <div className="form-actions">
            <button type="button" className="btn btn--sm btn--danger" onClick={() => void handleConfirm()} disabled={state.phase === 'busy'}>
              {state.phase === 'busy' ? '正在清空 …' : '确认清空'}
            </button>
            <button ref={cancelButton} type="button" className="btn btn--sm" onClick={handleCancel} disabled={state.phase === 'busy'}>
              取消
            </button>
          </div>
        </div>
      ) : (
        <div className="form-actions">
          <button ref={clearButton} type="button" className="btn btn--sm btn--danger" onClick={() => setState({ phase: 'confirming' })}>
            清空封面缓存
          </button>
        </div>
      )}

      <p className={state.phase === 'error' ? 'result result--err' : 'result result--dim'} style={mono} role="status" aria-live="polite">
        {statusText(state)}
      </p>
    </section>
  )
}
