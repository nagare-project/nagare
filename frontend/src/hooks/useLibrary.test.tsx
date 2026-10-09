// @vitest-environment jsdom
// 扫描后后台在识别作品时定时刷新，识别完就停 —— 用 fake timers 测启停与容错。
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchLibrary } from '../lib/endpoints'
import type { LibraryData } from '../lib/endpoints'
import { mountHook } from '../test/harness'
import { IDENTIFY_PAUSED_POLL_MS, IDENTIFY_POLL_MS, useLibrary } from './useLibrary'

vi.mock('../lib/endpoints', () => ({
  fetchLibrary: vi.fn(),
  addLibraryFolder: vi.fn(),
  removeLibraryFolder: vi.fn(),
  rescanLibrary: vi.fn(),
}))

const IDLE: LibraryData = { folders: [], clusters: [], continueWatching: [], scannedAt: 1 }
const RUNNING: LibraryData = { ...IDLE, identify: { running: true, done: 3, total: 10 } }
const PAUSED: LibraryData = { ...IDLE, identify: { running: false, done: 0, total: 0, error: 'animego 暂时连不上' } }

async function advance(ms: number): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

let visibility: DocumentVisibilityState = 'visible'
beforeEach(() => {
  vi.useFakeTimers()
  vi.mocked(fetchLibrary).mockReset()
  visibility = 'visible'
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility })
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useLibrary', () => {
  it('后台识别作品期间定时刷新，识别完的那次响应之后不再刷新', async () => {
    vi.mocked(fetchLibrary).mockResolvedValueOnce(RUNNING).mockResolvedValueOnce(RUNNING).mockResolvedValue(IDLE)
    const hook = await mountHook(() => useLibrary({ watchIdentify: true }))
    expect(fetchLibrary).toHaveBeenCalledTimes(1)

    await advance(IDENTIFY_POLL_MS)
    expect(fetchLibrary).toHaveBeenCalledTimes(2)
    await advance(IDENTIFY_POLL_MS)
    expect(fetchLibrary).toHaveBeenCalledTimes(3)
    expect(hook.result.current.state).toEqual({ phase: 'ready', data: IDLE })

    await advance(IDENTIFY_POLL_MS * 5)
    expect(fetchLibrary).toHaveBeenCalledTimes(3)
    await hook.unmount()
  })

  it('不显示识别进度的页面不轮询', async () => {
    vi.mocked(fetchLibrary).mockResolvedValue(RUNNING)
    const hook = await mountHook(() => useLibrary())
    await advance(IDENTIFY_POLL_MS * 5)
    expect(fetchLibrary).toHaveBeenCalledTimes(1)
    await hook.unmount()
  })

  it('识别暂停（animego 不可用）时放慢刷新，等后端到点恢复', async () => {
    vi.mocked(fetchLibrary).mockResolvedValueOnce(PAUSED).mockResolvedValue(IDLE)
    const hook = await mountHook(() => useLibrary({ watchIdentify: true }))
    await advance(IDENTIFY_POLL_MS * 3)
    expect(fetchLibrary).toHaveBeenCalledTimes(1)
    await advance(IDENTIFY_PAUSED_POLL_MS)
    expect(fetchLibrary).toHaveBeenCalledTimes(2)
    expect(hook.result.current.state).toEqual({ phase: 'ready', data: IDLE })
    await hook.unmount()
  })

  it('后台刷新失败时保留已经显示的内容，不换成错误页', async () => {
    vi.mocked(fetchLibrary).mockResolvedValueOnce(RUNNING).mockRejectedValue(new Error('服务重启中'))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const hook = await mountHook(() => useLibrary({ watchIdentify: true }))
    await advance(IDENTIFY_POLL_MS)
    expect(fetchLibrary).toHaveBeenCalledTimes(2)
    expect(hook.result.current.state).toEqual({ phase: 'ready', data: RUNNING })
    await hook.unmount()
  })

  it('晚到的旧响应不覆盖新数据', async () => {
    let resolveSlow!: (data: LibraryData) => void
    vi.mocked(fetchLibrary)
      .mockReturnValueOnce(new Promise((done) => { resolveSlow = done }))
      .mockResolvedValueOnce(IDLE)
    const hook = await mountHook(() => useLibrary())
    await act(async () => { await hook.result.current.reload() })
    expect(hook.result.current.state).toEqual({ phase: 'ready', data: IDLE })
    await act(async () => resolveSlow(RUNNING))
    expect(hook.result.current.state).toEqual({ phase: 'ready', data: IDLE })
    await hook.unmount()
  })

  it('标签页不可见时不刷新，切回来立刻补一次', async () => {
    vi.mocked(fetchLibrary).mockResolvedValue(RUNNING)
    const hook = await mountHook(() => useLibrary({ watchIdentify: true }))
    visibility = 'hidden'
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')) })
    await advance(IDENTIFY_POLL_MS * 5)
    expect(fetchLibrary).toHaveBeenCalledTimes(1)
    visibility = 'visible'
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')) })
    expect(fetchLibrary).toHaveBeenCalledTimes(2)
    await hook.unmount()
  })
})
