import { apiFetch } from './api'

/**
 * 补发「看完了、还没记到账号」的集。后端在启动时自动补一轮；媒体库横幅上的「重试」当场再补一轮。
 * 每集走的是与播放结束时完全相同的检查（作品关联、跨季集号换算），一轮最多十来集。
 */

/** POST /api/player/sync/retry 的结果 */
export interface SyncRetryResult {
  /** 这一轮补记上的集数 */
  synced: number
  /** 之后还会再试的集数（下次启动或再点「重试」） */
  remaining: number
  /** 这一轮为什么没补完（没登录、连不上、被限流、登录过期）；补完了或只是到了单轮上限时缺席 */
  reason?: string
}

export function retryPendingSyncs(): Promise<SyncRetryResult> {
  return apiFetch<SyncRetryResult>('/api/player/sync/retry', { method: 'POST' })
}

/** 把一轮补发的结果说成状态行上的一句话 */
export function describeSyncRetry(result: SyncRetryResult): { tone: 'ok' | 'err' | 'dim'; text: string } {
  const { synced, remaining, reason } = result
  if (reason !== undefined && reason !== '') {
    return { tone: 'err', text: synced > 0 ? `补记上 ${synced} 集；其余没补完：${reason}` : reason }
  }
  if (synced > 0) {
    return remaining > 0
      ? { tone: 'ok', text: `补记上 ${synced} 集，还有 ${remaining} 集没记上：再点一次「重试」，或等下次启动时补记` }
      : { tone: 'ok', text: `补记上 ${synced} 集，都记到账号了` }
  }
  return remaining > 0
    ? { tone: 'dim', text: `这次没能补记，还有 ${remaining} 集等着：下次启动时会再试` }
    : { tone: 'dim', text: '没有需要补记的集' }
}
