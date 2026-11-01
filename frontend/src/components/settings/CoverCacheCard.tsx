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

/** 清空之后的一句话；有正在显示的封面没删掉时说一声 */
export function clearedText(result: CoverCacheClearResult): string {
  const base = `已清空 · 当前占用 ${formatBytes(result.bytes)}`
  return result.skipped > 0 ? `${base}（${result.skipped} 张正在显示，稍后可再清一次）` : base
}

/**
 * 「封面缓存」卡：发现页与媒体库的海报图存在本机，这里看占用、手动清空。
 * 二次确认做成内联（与退出卡一致，不用 window.confirm）。
 */
export function CoverCacheCard({ cache, onReload }: CoverCacheCardProps) {
  const [state, setState] = useState<ClearState>({ phase: 'idle' })
  const clearButton = useRef<HTMLButtonElement>(null)
  const cancelButton = useRef<HTMLButtonElement>(null)
  // 只在用户自己从确认区退出来时把焦点还给「清空」按钮；首次渲染不抢焦点
  const returnFocus = useRef(false)
  const confirming = state.phase === 'confirming' || state.phase === 'busy'

  useEffect(() => {
    if (state.phase === 'confirming') {
      cancelButton.current?.focus()
    } else if (!confirming && returnFocus.current) {
      returnFocus.current = false
      clearButton.current?.focus()
    }
  }, [state.phase, confirming])

  async function handleConfirm(): Promise<void> {
    setState({ phase: 'busy' })
    returnFocus.current = true
    try {
      const result = await clearCoverCache()
      setState({ phase: 'done', result })
      await onReload()
    } catch (err) {
      console.error('清空封面缓存失败', err)
      setState({ phase: 'error', message: errorText(err, '清空封面缓存失败') })
    }
  }

  function handleCancel(): void {
    returnFocus.current = true
    setState({ phase: 'idle' })
  }

  return (
    <section className="panel settings-card" aria-labelledby="cover-cache-heading">
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
        {state.phase === 'done' ? clearedText(state.result) : state.phase === 'error' ? state.message : ''}
      </p>
    </section>
  )
}
