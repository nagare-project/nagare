import { useEffect, useRef, useState } from 'react'
import { copyText } from '../../lib/clipboard'
import type { SearchItem } from '../../lib/endpoints'
import { mono } from '../../tokens'
import './search.css'

/** 播放按钮禁用态的提示（M3 之前磁力不能播） */
export const PLAY_UNAVAILABLE_HINT = '边下边播在下一里程碑（M3）上线，目前请复制磁力链接到其他下载器'

/** 「已复制」提示停留多久后恢复按钮文案 */
const COPY_FEEDBACK_MS = 1800

type CopyState = 'idle' | 'copied' | 'failed'

const COPY_LABEL: Record<CopyState, string> = {
  idle: '复制磁力',
  copied: '已复制 ✓',
  failed: '复制失败',
}

export interface ResultRowProps {
  item: SearchItem
  /** 来源规则的展示名（查不到时传 id） */
  sourceName: string
  /** 表头是否有「做种」列（整表统一，由 ResultTable 决定） */
  showSeeders: boolean
}

/**
 * 结果表的一行：标题（可换行）· 体积 · 字幕组 · 日期（原样）· 做种（可选）· 来源 · 操作。
 * 「复制磁力」走 lib/clipboard 的 copyText；「播放」禁用并提示 M3。
 */
export function ResultRow({ item, sourceName, showSeeders }: ResultRowProps) {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const timerRef = useRef<number | null>(null)

  // 卸载时清掉复位定时器，别对着已卸载的行 setState
  useEffect(() => {
    return () => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    }
  }, [])

  async function handleCopy(): Promise<void> {
    setCopyState((await copyText(item.magnet)) ? 'copied' : 'failed')
    if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    timerRef.current = window.setTimeout(() => setCopyState('idle'), COPY_FEEDBACK_MS)
  }

  const copyClass =
    copyState === 'failed'
      ? 'hud-button hud-button--small res-copy res-copy--failed'
      : 'hud-button hud-button--small res-copy'

  return (
    <tr className="res-row" data-source={item.source}>
      <td className="res-title">
        <span className="res-title-text">{item.title}</span>
        {item.provider !== undefined && item.provider !== '' && (
          <span className="badge res-provider" title="上游站点">
            {item.provider}
          </span>
        )}
      </td>
      <td className="res-size" style={mono}>
        {item.size}
      </td>
      <td className="res-fansub" title={item.fansub ?? undefined}>
        {item.fansub ?? ''}
      </td>
      <td className="res-date" style={mono}>
        {item.date ?? ''}
      </td>
      {showSeeders && (
        <td className="res-seeders" style={mono}>
          {item.seeders ?? ''}
        </td>
      )}
      <td className="res-source">
        <span className="badge badge--accent">{sourceName}</span>
      </td>
      <td className="res-actions">
        <button
          type="button"
          className={copyClass}
          onClick={() => void handleCopy()}
          aria-label={`复制磁力链接：${item.title}`}
        >
          {COPY_LABEL[copyState]}
        </button>
        <span className="visually-hidden" role="status" aria-live="polite">
          {copyState === 'idle' ? '' : COPY_LABEL[copyState]}
        </span>
        <span className="res-play-wrap" title={PLAY_UNAVAILABLE_HINT}>
          <button
            type="button"
            className="hud-button hud-button--small hud-button--ghost"
            disabled
            aria-label={`播放（${PLAY_UNAVAILABLE_HINT}）`}
          >
            播放
          </button>
        </span>
      </td>
    </tr>
  )
}
