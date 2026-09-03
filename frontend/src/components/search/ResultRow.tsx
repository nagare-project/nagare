import { useEffect, useRef, useState } from 'react'
import { copyText } from '../../lib/clipboard'
import type { SearchItem } from '../../lib/endpoints'
import { mono } from '../../tokens'
import './search.css'

/** 「已复制」提示停留多久后恢复按钮文案 */
const COPY_FEEDBACK_MS = 1800

type CopyState = 'idle' | 'copied' | 'failed'

const COPY_LABEL: Record<CopyState, string> = {
  idle: '复制磁力',
  copied: '已复制 ✓',
  failed: '复制失败',
}

/**
 * 一行的播放按钮状态：
 * idle 可点 · pending 本行正在起播 · active 本行正在边下边播 ·
 * blocked 别的行占着后端 · disabled 磁力引擎不可用
 */
export type PlayStage = 'idle' | 'pending' | 'active' | 'blocked' | 'disabled'

/** 同一时刻只允许一条磁力占着后端 */
export const PLAY_BLOCKED_HINT = '已有一条磁力在播放，先停止底部状态条里的那条再播这条'

/** 引擎起不来时的按钮提示（详细原因与恢复动作在设置页） */
export const PLAY_ENGINE_DOWN_HINT = '磁力播放未启用，请查看设置页'

const PLAY_LABEL: Record<PlayStage, string> = {
  idle: '播放',
  pending: '启动中 …',
  active: '播放中',
  blocked: '播放',
  disabled: '播放',
}

/** 禁用态各自的原因；idle 为 null 表示可点 */
const PLAY_HINT: Record<PlayStage, string | null> = {
  idle: null,
  pending: '正在等待元数据与起播缓冲，进度见底部状态条',
  active: '这条磁力正在播放，停止请用底部状态条',
  blocked: PLAY_BLOCKED_HINT,
  disabled: PLAY_ENGINE_DOWN_HINT,
}

/** 磁力播放的当前占用情况，由搜索页从 useTorrentPlay 推导后传下来 */
export interface PlayControl {
  /**
   * 播放这一条。第二个参数是被点下的那个按钮元素 —— 选集弹窗关闭后要把焦点
   * 交还给它，而弹窗自己读 document.activeElement 是读不到的（按钮点下即禁用）。
   */
  onPlay: (item: SearchItem, trigger: HTMLButtonElement) => void
  /** 正占着后端的那条磁力；null = 空闲 */
  busy: { magnet: string; stage: 'pending' | 'active' } | null
  /** 磁力引擎不可用（后端 torrent.enabled=false）：整列禁用 */
  engineDown: boolean
}

export interface ResultRowProps {
  item: SearchItem
  /** 来源规则的展示名（查不到时传 id） */
  sourceName: string
  /** 表头是否有「做种」列（整表统一，由 ResultTable 决定） */
  showSeeders: boolean
  play: PlayControl
}

/** 本行的播放按钮该是什么状态 */
export function playStage(magnet: string, play: PlayControl): PlayStage {
  if (play.engineDown) return 'disabled'
  if (play.busy === null) return 'idle'
  return play.busy.magnet === magnet ? play.busy.stage : 'blocked'
}

/**
 * 结果表的一行：标题（可换行）· 体积 · 字幕组 · 日期（原样）· 做种（可选）· 来源 · 操作。
 * 「复制磁力」走 lib/clipboard 的 copyText；「播放」把这条磁力交给 useTorrentPlay，
 * 进行中 / 被别的行占着 / 引擎不可用时禁用，并把原因写在 title 与 aria-label 里。
 */
export function ResultRow({ item, sourceName, showSeeders, play }: ResultRowProps) {
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

  const stage = playStage(item.magnet, play)
  const hint = PLAY_HINT[stage]

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
        {/* 禁用的按钮收不到 hover 事件，title 挂在外层 span 上才提示得出来 */}
        <span className="res-play-wrap" title={hint ?? undefined}>
          <button
            type="button"
            className={
              stage === 'active'
                ? 'hud-button hud-button--small res-play res-play--active'
                : 'hud-button hud-button--small res-play'
            }
            onClick={(event) => play.onPlay(item, event.currentTarget)}
            disabled={stage !== 'idle'}
            aria-label={hint === null ? `播放：${item.title}` : `播放（${hint}）`}
          >
            {PLAY_LABEL[stage]}
          </button>
        </span>
      </td>
    </tr>
  )
}
