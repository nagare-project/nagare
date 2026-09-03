import { useEffect, useRef } from 'react'
import type { KeyboardEvent } from 'react'
import type { TorrentFile } from '../../lib/endpoints'
import { formatBytes, formatEpisode } from '../../lib/format'
import { mono } from '../../tokens'
import './torrent.css'

/** 弹窗内可聚焦元素的选择器（Tab 循环用） */
const FOCUSABLE = 'button:not(:disabled)'

export interface EpisodePickerProps {
  /** 这条磁力的展示名（搜索结果标题） */
  title: string
  files: TorrentFile[]
  onSelect: (fileIndex: number) => void
  /** 取消：调用方必须同时 POST stop，把后端保留着的种子放掉 */
  onCancel: () => void
  /**
   * 弹窗关闭后把焦点交还给谁，由调用方决定。
   *
   * 这里【不能】自己读 document.activeElement 来记「谁打开了我」：触发它的那个
   * 播放按钮在点下的同一次渲染里就自我禁用了，禁用元素留不住焦点，等这个弹窗
   * 挂载时 activeElement 已经是 <body>，关闭时再 focus 它等于把焦点丢了。
   */
  restoreFocus?: () => void
}

/**
 * 选集弹窗：后端在合集里定位不到唯一文件时弹出，列出候选文件让用户挑。
 *
 * 可达性：role=dialog + aria-modal + 有可访问名；打开时焦点进入弹窗、
 * Tab 在弹窗内循环、Esc 关闭、关闭后焦点回到打开它的元素。
 * 刻意不做「点遮罩关闭」—— 关闭会释放后端已经下好元数据的种子，误触代价太大。
 */
export function EpisodePicker({
  title,
  files,
  onSelect,
  onCancel,
  restoreFocus,
}: EpisodePickerProps) {
  const dialogRef = useRef<HTMLDivElement | null>(null)
  // 走 ref：调用方每次渲染传新的函数字面量也不该让下面这个 effect 重跑
  //（重跑会把焦点又拽回第一个候选项）。
  const restoreRef = useRef(restoreFocus)
  useEffect(() => {
    restoreRef.current = restoreFocus
  }, [restoreFocus])

  useEffect(() => {
    // 焦点先落到第一个候选项：键盘用户打开即可上下选，不用先 Tab 一圈
    const first = dialogRef.current?.querySelector<HTMLElement>(FOCUSABLE)
    first?.focus()
    return () => {
      // 推迟一帧：卸载清理跑在提交新 DOM 之前，此刻按钮的禁用状态还是旧的，
      // 立刻 focus 会落在一个马上要被禁用的元素上。
      const restore = restoreRef.current
      if (restore === undefined) return
      window.requestAnimationFrame(restore)
    }
  }, [])

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>): void {
    if (event.key === 'Escape') {
      event.stopPropagation()
      onCancel()
      return
    }
    if (event.key !== 'Tab') return
    const items = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])
    if (items.length === 0) return
    const firstItem = items[0]
    const lastItem = items[items.length - 1]
    if (firstItem === undefined || lastItem === undefined) return
    // 焦点撞到两端时手动绕回来，别让 Tab 跑到弹窗背后的页面上
    if (event.shiftKey && document.activeElement === firstItem) {
      event.preventDefault()
      lastItem.focus()
    } else if (!event.shiftKey && document.activeElement === lastItem) {
      event.preventDefault()
      firstItem.focus()
    }
  }

  return (
    <div className="ep-overlay">
      <div
        ref={dialogRef}
        className="panel ep-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="episode-picker-heading"
        aria-describedby="episode-picker-desc"
        onKeyDown={handleKeyDown}
      >
        <h2 id="episode-picker-heading" className="ep-heading">
          选择要播放的剧集
        </h2>
        <p id="episode-picker-desc" className="ep-desc">
          这条资源里有多个可播放的文件，nagare 没能确定是哪一集。选一个开始播放。
        </p>
        <p className="ep-source" style={mono} title={title}>
          {title}
        </p>

        {files.length === 0 ? (
          <p className="result result--err" style={mono} role="alert">
            这条资源里没有可播放的视频文件，请取消后换一条。
          </p>
        ) : (
          <ul className="ep-list">
            {files.map((file) => (
              <li key={file.index}>
                <button
                  type="button"
                  className="ep-item"
                  onClick={() => onSelect(file.index)}
                  aria-label={`播放 ${file.name}`}
                >
                  <span className="ep-ep" style={mono}>
                    {episodeText(file)}
                  </span>
                  <span className="ep-name">{file.name}</span>
                  <span className="ep-size" style={mono}>
                    {formatBytes(file.sizeBytes)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}

        <div className="form-actions ep-actions">
          <button
            type="button"
            className="hud-button hud-button--small hud-button--ghost"
            onClick={onCancel}
          >
            取消
          </button>
          <span className="result result--dim ep-hint">取消会放弃这条磁力并释放已下载的分片</span>
        </div>
      </div>
    </div>
  )
}

/** 集号列：解析不出就留空（后端 episode 可能是 null，也可能整个字段缺席） */
function episodeText(file: TorrentFile): string {
  return typeof file.episode === 'number' ? formatEpisode(file.episode) : ''
}
