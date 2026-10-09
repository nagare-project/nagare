import { useEffect, useEffectEvent } from 'react'
import type { LibraryState } from './useLibrary'
import type { ScanStats } from '../lib/endpoints'

/**
 * 距上次扫描超过这么久，回到媒体库页（打开、切回标签页）时自动重扫一次。
 * 对应 animego「我的库」的 useAutoRescan（挂载、切回标签页时扫描，最短间隔 60 秒）：
 * nagare 常年挂在后台，新下载的集要等用户手点「重新扫描」才出现，是最常被问的一件事。
 * 扫描只读目录与文件大小，不读内容；认作品在后台按限速进行，不受它影响。
 */
export const AUTO_RESCAN_AFTER_MS = 2 * 60 * 1000

// 放在模块里而不是组件实例里：来回切页面会反复挂载媒体库页，按实例记的话每次挂载都能再扫一次
let lastAttempt = 0
let inFlight = false

/** 测试用：清掉模块里的节流记录 */
export function resetAutoRescanForTest(): void {
  lastAttempt = 0
  inFlight = false
}

/**
 * 在媒体库页挂载、标签页重新可见时，按需自动重扫。两次尝试至少间隔 AUTO_RESCAN_AFTER_MS ——
 * 扫描结果的时间戳不前进（比如目录在拔掉的盘上）也不会反复扫。
 */
export function useAutoRescan(state: LibraryState, rescan: () => Promise<ScanStats>, onError: (err: unknown) => void): void {
  const maybeRescan = useEffectEvent(() => {
    if (document.visibilityState === 'hidden' || inFlight) return
    if (state.phase !== 'ready' || state.data.folders.length === 0) return
    const now = Date.now()
    if (now - (state.data.scannedAt ?? 0) < AUTO_RESCAN_AFTER_MS || now - lastAttempt < AUTO_RESCAN_AFTER_MS) return
    lastAttempt = now
    inFlight = true
    rescan().catch(onError).finally(() => { inFlight = false })
  })

  useEffect(() => {
    const listener = () => maybeRescan()
    document.addEventListener('visibilitychange', listener)
    return () => document.removeEventListener('visibilitychange', listener)
  }, [])

  const ready = state.phase === 'ready'
  useEffect(() => {
    if (ready) maybeRescan()
  }, [ready])
}
