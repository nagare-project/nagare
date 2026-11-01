import { useState } from 'react'
import type { PlayerStatus } from '../../lib/endpoints'

interface SyncAlertProps {
  status: PlayerStatus | null
  /** 横幅上的「重试」：当场补发一轮，结果由调用方说出来并刷新播放器状态 */
  onRetry: () => Promise<void>
}

/**
 * 「这一集没记到你的账号」。
 *
 * 为什么值得一条常驻横幅：回写失败发生在 mpv 已经退出【之后】，
 * 那条路径上界面不会有任何变化 —— 用户以为记上了，下次打开网站
 * 才发现还停在上一集，而那时已经不知道是哪一集丢的。
 *
 * 不做「知道了」按钮：它描述的不是一条过时的通知，而是一个仍然成立的事实
 *（那一集确实还没同步）。回写成功后后端自己会把它清掉，横幅随之消失。
 *
 * 没记上的集下次启动时会自动补记；还有会再试的集（pending > 0）时给「重试」当场再补。
 * 被规则挡下的那种（集号换算不出来）补发不会再试，只能按恢复动作手动去标，所以不给按钮。
 */
export function SyncAlert({ status, onRetry }: SyncAlertProps) {
  const [busy, setBusy] = useState(false)
  const sync = status?.sync
  if (sync === undefined) return null
  const what =
    sync.title !== undefined && sync.title !== ''
      ? `「${sync.title}${sync.episode !== undefined && sync.episode > 0 ? ` 第${sync.episode}集` : ''}」`
      : '刚看完的这一集'
  const pending = sync.pending ?? 0

  async function handleRetry(): Promise<void> {
    setBusy(true)
    try {
      await onRetry()
    } finally {
      setBusy(false)
    }
  }

  return (
    <p className="alert-warn" role="status">
      {what}看完了，但没能记到你的 animego 账号
      {sync.reason !== undefined && sync.reason !== '' ? `：${sync.reason}` : '。'}
      {sync.recovery !== undefined && sync.recovery !== '' && (
        <span className="alert-warn-recovery">→ {sync.recovery}</span>
      )}
      {pending > 0 && (
        <span className="alert-warn-recovery lib-sync-retry">
          <span>共有 {pending} 集等着补记，下次启动 nagare 时会自动补上。</span>
          <button type="button" className="btn btn--sm" onClick={() => void handleRetry()} disabled={busy}>
            {busy ? '重试中 …' : '重试'}
          </button>
        </span>
      )}
    </p>
  )
}
