// @vitest-environment jsdom
// 轮询 hook 依赖 react-dom 渲染与定时器，用 jsdom + fake timers 测启停行为。
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PlayerStatus } from '../lib/endpoints'
import { mountHook } from '../test/harness'
import { usePlayerStatus } from './usePlayerStatus'

const PLAYING: PlayerStatus = {
  playing: true,
  fileId: 'file-1',
  title: '葬送的芙莉莲 第01集',
  position: 12,
  duration: 1420,
  paused: false,
  danmaku: { state: 'ok', count: 3210 },
}

const STOPPED: PlayerStatus = { playing: false }

const INTERVAL = 2000

/** 推进假时钟并让期间触发的异步 setState 落定 */
async function advance(ms: number): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('usePlayerStatus', () => {
  it('挂载先拉一次；playing 时每个间隔轮询一次', async () => {
    const fetcher = vi.fn<() => Promise<PlayerStatus>>().mockResolvedValue(PLAYING)
    const hook = await mountHook(() => usePlayerStatus({ fetcher, intervalMs: INTERVAL }))

    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(hook.result.current.status).toEqual(PLAYING)

    await advance(INTERVAL)
    expect(fetcher).toHaveBeenCalledTimes(2)

    await advance(INTERVAL * 2)
    expect(fetcher).toHaveBeenCalledTimes(4)

    await hook.unmount()
  })

  it('拉到「未播放」后停表，不再轮询', async () => {
    let current: PlayerStatus = PLAYING
    const fetcher = vi.fn(async () => current)
    const hook = await mountHook(() => usePlayerStatus({ fetcher, intervalMs: INTERVAL }))
    expect(fetcher).toHaveBeenCalledTimes(1)

    current = STOPPED
    await advance(INTERVAL) // 这一拍拉到 stopped
    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(hook.result.current.status).toEqual(STOPPED)

    await advance(INTERVAL * 5) // 之后完全静默
    expect(fetcher).toHaveBeenCalledTimes(2)

    await hook.unmount()
  })

  it('未播放时不轮询；refresh() 拉到 playing 后自动恢复轮询', async () => {
    let current: PlayerStatus = STOPPED
    const fetcher = vi.fn(async () => current)
    const hook = await mountHook(() => usePlayerStatus({ fetcher, intervalMs: INTERVAL }))
    expect(fetcher).toHaveBeenCalledTimes(1)

    await advance(INTERVAL * 3)
    expect(fetcher).toHaveBeenCalledTimes(1) // 静止期没有轮询

    current = PLAYING
    await act(async () => {
      await hook.result.current.refresh()
    })
    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(hook.result.current.status).toEqual(PLAYING)

    await advance(INTERVAL)
    expect(fetcher).toHaveBeenCalledTimes(3) // 轮询已恢复

    await hook.unmount()
  })

  it('apply() 乐观置为播放后，轮询自动启动（POST /api/play 成功路径）', async () => {
    const fetcher = vi.fn<() => Promise<PlayerStatus>>().mockResolvedValue(STOPPED)
    const hook = await mountHook(() => usePlayerStatus({ fetcher, intervalMs: INTERVAL }))
    expect(fetcher).toHaveBeenCalledTimes(1)

    fetcher.mockResolvedValue(PLAYING)
    await act(async () => {
      hook.result.current.apply(PLAYING)
    })
    expect(hook.result.current.status).toEqual(PLAYING)

    await advance(INTERVAL)
    expect(fetcher).toHaveBeenCalledTimes(2) // 乐观状态唤醒了轮询

    await hook.unmount()
  })

  it('拉取失败：错误透出、状态置未知、轮询停止', async () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const fetcher = vi
      .fn<() => Promise<PlayerStatus>>()
      .mockResolvedValueOnce(PLAYING)
      .mockRejectedValue(new Error('无法连接到 nagare 后端，请确认本地服务已启动'))
    const hook = await mountHook(() => usePlayerStatus({ fetcher, intervalMs: INTERVAL }))
    expect(hook.result.current.status).toEqual(PLAYING)

    await advance(INTERVAL) // 这一拍失败
    expect(hook.result.current.status).toBe(null)
    expect(hook.result.current.error).toBe('无法连接到 nagare 后端，请确认本地服务已启动')
    expect(consoleSpy).toHaveBeenCalled()

    await advance(INTERVAL * 5) // 状态未知 → 不再轮询刷屏
    expect(fetcher).toHaveBeenCalledTimes(2)

    await hook.unmount()
  })
})
