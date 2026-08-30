import type { DanmakuStatus, PlayingStatus } from '../../lib/endpoints'
import { formatDuration, progressPercent } from '../../lib/format'
import { mono } from '../../tokens'
import './now-playing.css'

export interface NowPlayingBarProps {
  status: PlayingStatus
  /** 暂停/停止请求在途时禁用按钮，防连点 */
  busy: boolean
  onTogglePause: (paused: boolean) => void
  onStop: () => void
}

/**
 * 底部固定的「正在播放」条：脉冲点 + 标题 + 弹幕徽标 + 时钟 + 暂停/停止。
 * 顶缘 2px 细线显示整体播放进度（宽度动画走 compositor 友好的 width 小面积
 * 元素，1s linear 与轮询节奏匹配）。
 */
export function NowPlayingBar({ status, busy, onTogglePause, onStop }: NowPlayingBarProps) {
  const { title, position, duration, paused, danmaku } = status
  const pct = progressPercent(position, duration)

  return (
    <footer className="np-bar" aria-label="正在播放">
      <div className="np-track" aria-hidden="true">
        <div className="np-track-fill" style={{ width: `${pct}%` }} />
      </div>

      <span className={paused ? 'np-pulse np-pulse--paused' : 'np-pulse'} aria-hidden="true" />

      <div className="np-main">
        <span className="np-title" title={title}>
          {title}
        </span>
        <DanmakuBadge danmaku={danmaku} />
      </div>

      <span className="np-time" style={mono}>
        {formatDuration(position)} / {formatDuration(duration)}
      </span>

      <div className="np-actions">
        <button
          type="button"
          className="hud-button hud-button--small"
          onClick={() => onTogglePause(!paused)}
          disabled={busy}
        >
          {paused ? '继续' : '暂停'}
        </button>
        <button
          type="button"
          className="hud-button hud-button--small hud-button--ghost"
          onClick={onStop}
          disabled={busy}
        >
          停止
        </button>
      </div>
    </footer>
  )
}

/**
 * 弹幕状态徽标：
 * loading → 「弹幕匹配中…」（后端先起 mpv、弹幕后台解析，轮询会刷新成终态）；
 * ok → 「弹幕 N 条」；unmatched → 「未匹配」；
 * unavailable / degraded / none → 「弹幕不可用」，reason 挂 tooltip。
 */
export function DanmakuBadge({ danmaku }: { danmaku: DanmakuStatus }) {
  switch (danmaku.state) {
    case 'loading':
      return (
        <span className="badge" title={danmaku.reason ?? undefined}>
          弹幕匹配中…
        </span>
      )
    case 'ok':
      return <span className="badge badge--accent">弹幕 {danmaku.count ?? 0} 条</span>
    case 'unmatched':
      return (
        <span className="badge badge--warn" title="未能匹配到对应剧集的弹幕">
          未匹配
        </span>
      )
    case 'unavailable':
    case 'degraded':
    case 'none':
      return (
        <span className="badge" title={danmaku.reason ?? undefined}>
          弹幕不可用
        </span>
      )
  }
}
