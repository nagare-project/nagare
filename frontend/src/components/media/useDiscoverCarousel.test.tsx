// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mountHook } from '../../test/harness'
import { DISCOVER_EXIT_MS, DISCOVER_INTERVAL_MS, useDiscoverCarousel } from './useDiscoverCarousel'

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())
async function advance(ms: number) { await act(async () => { vi.advanceTimersByTime(ms) }) }

describe('Discover 轮播时序', () => {
  it('保持旧背景直到退场结束，随后自动进入下一部', async () => {
    const { result, unmount } = await mountHook(() => useDiscoverCarousel(4, false, false))
    await advance(DISCOVER_INTERVAL_MS)
    expect(result.current.activeIndex).toBe(0)
    expect(result.current.transitioning).toBe(true)
    await advance(DISCOVER_EXIT_MS)
    expect(result.current.activeIndex).toBe(1)
    expect(result.current.transitioning).toBe(false)
    await unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('快速连点只展示最后一次选择；点回当前项可取消切换', async () => {
    const { result, unmount } = await mountHook(() => useDiscoverCarousel(4, true, false))
    await act(async () => result.current.select(1))
    await advance(400)
    await act(async () => result.current.select(3))
    await advance(500)
    expect(result.current.activeIndex).toBe(0)
    await advance(400)
    expect(result.current.activeIndex).toBe(3)
    await act(async () => result.current.select(2))
    await act(async () => result.current.select(3))
    expect(result.current.transitioning).toBe(false)
    await advance(DISCOVER_EXIT_MS)
    expect(result.current.activeIndex).toBe(3)
    await unmount()
  })

  it('悬停或暂停时不自动切换，减少动态效果时手动切换立即完成', async () => {
    const stopped = await mountHook(() => useDiscoverCarousel(4, true, false))
    await advance(DISCOVER_INTERVAL_MS * 2)
    expect(stopped.result.current.activeIndex).toBe(0)
    await stopped.unmount()
    const reduced = await mountHook(() => useDiscoverCarousel(4, false, true))
    await act(async () => reduced.result.current.select(2))
    expect(reduced.result.current.activeIndex).toBe(2)
    expect(reduced.result.current.transitioning).toBe(false)
    await advance(DISCOVER_INTERVAL_MS * 2)
    expect(reduced.result.current.activeIndex).toBe(2)
    await reduced.unmount()
  })
})
