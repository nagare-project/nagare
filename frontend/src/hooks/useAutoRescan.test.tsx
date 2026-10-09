// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { LibraryData } from '../lib/endpoints'
import { mountHook } from '../test/harness'
import { AUTO_RESCAN_AFTER_MS, resetAutoRescanForTest, useAutoRescan } from './useAutoRescan'
import type { LibraryState } from './useLibrary'

const NOW = 1_800_000_000_000
const ready = (scannedAt: number | null, folders = 1): LibraryState => ({
  phase: 'ready',
  data: { folders: Array.from({ length: folders }, (_, i) => ({ id: `f${i}`, path: `/m/${i}`, addedAt: 1 })), clusters: [], continueWatching: [], scannedAt } satisfies LibraryData,
})

let visibility: DocumentVisibilityState = 'visible'
beforeEach(() => {
  resetAutoRescanForTest()
  vi.useFakeTimers()
  vi.setSystemTime(NOW)
  visibility = 'visible'
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility })
})
afterEach(() => {
  vi.useRealTimers()
})

async function becomeVisible(): Promise<void> {
  visibility = 'visible'
  await act(async () => { document.dispatchEvent(new Event('visibilitychange')) })
}

describe('useAutoRescan', () => {
  it('挂载时上次扫描已久就重扫一次；刚扫过、没有文件夹时不扫', async () => {
    const rescan = vi.fn().mockResolvedValue({ videos: 1, clusters: 1 })
    const stale = await mountHook(() => useAutoRescan(ready(NOW - AUTO_RESCAN_AFTER_MS - 1), rescan, vi.fn()))
    expect(rescan).toHaveBeenCalledOnce()
    await stale.unmount()

    rescan.mockClear()
    const fresh = await mountHook(() => useAutoRescan(ready(NOW - 1000), rescan, vi.fn()))
    const empty = await mountHook(() => useAutoRescan(ready(null, 0), rescan, vi.fn()))
    expect(rescan).not.toHaveBeenCalled()
    await fresh.unmount()
    await empty.unmount()
  })

  it('切回标签页时按需重扫，两次尝试至少隔开间隔（扫描时间不前进也不会反复扫）', async () => {
    const rescan = vi.fn().mockResolvedValue({ videos: 1, clusters: 1 })
    const old = ready(NOW - AUTO_RESCAN_AFTER_MS * 10)
    const hook = await mountHook(() => useAutoRescan(old, rescan, vi.fn()))
    expect(rescan).toHaveBeenCalledTimes(1)
    await becomeVisible()
    expect(rescan).toHaveBeenCalledTimes(1)
    vi.setSystemTime(NOW + AUTO_RESCAN_AFTER_MS + 1)
    await becomeVisible()
    expect(rescan).toHaveBeenCalledTimes(2)
    await hook.unmount()
  })

  it('失败交给调用方显示', async () => {
    const failure = new Error('扫描失败')
    const onError = vi.fn()
    const hook = await mountHook(() => useAutoRescan(ready(0), vi.fn().mockRejectedValue(failure), onError))
    await act(async () => { await Promise.resolve() })
    expect(onError).toHaveBeenCalledWith(failure)
    await hook.unmount()
  })
})
