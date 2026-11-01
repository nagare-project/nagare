// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { BANNER_INTERVAL_FALLBACK_MS, bannerInterval, useBannerRotation } from './useBannerRotation'
import type { BannerRotationOptions } from './useBannerRotation'

const INTERVAL = BANNER_INTERVAL_FALLBACK_MS
beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  vi.useRealTimers()
  setVisibility('visible')
  document.documentElement.style.removeProperty('--dur-banner')
})
async function advance(ms: number) { await act(async () => { vi.advanceTimersByTime(ms) }) }

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state })
  document.dispatchEvent(new Event('visibilitychange'))
}

type Rotation = ReturnType<typeof useBannerRotation>

function Probe({ options, result }: { options: BannerRotationOptions; result: { current: Rotation | undefined } }) {
  result.current = useBannerRotation(options)
  return null
}

/** 可以在测试中途改参数的 hook 探针 */
async function mountRotation(initial: Partial<BannerRotationOptions> = {}) {
  let options: BannerRotationOptions = { count: 3, held: false, paused: false, reduced: false, element: null, ...initial }
  const holder: { current: Rotation | undefined } = { current: undefined }
  const view = await mount(<Probe options={options} result={holder} />)
  return {
    result: holder as { current: Rotation },
    unmount: view.unmount,
    async set(next: Partial<BannerRotationOptions>) {
      options = { ...options, ...next }
      await view.rerender(<Probe options={options} result={holder} />)
    },
  }
}

describe('横幅轮换的计时', () => {
  it('间隔从 --dur-banner 读，读不到时用 8 秒', () => {
    expect(bannerInterval()).toBe(INTERVAL)
    document.documentElement.style.setProperty('--dur-banner', '6000ms')
    expect(bannerInterval()).toBe(6000)
    document.documentElement.style.setProperty('--dur-banner', '2.5s')
    expect(bannerInterval()).toBe(2500)
  })

  it('每满一个间隔换下一张，到头回到第一张；卸载时不留计时器', async () => {
    const { result, unmount } = await mountRotation()
    await advance(INTERVAL - 1)
    expect(result.current.index).toBe(0)
    await advance(1)
    expect(result.current.index).toBe(1)
    // 每次切换后，下一段计时在重新渲染之后才排上：逐段推进
    await advance(INTERVAL)
    expect(result.current.index).toBe(2)
    await advance(INTERVAL)
    expect(result.current.index).toBe(0)
    await unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('停住之后接着剩下的时间走，而不是重新给满', async () => {
    const view = await mountRotation()
    await advance(INTERVAL - 2000)
    await view.set({ held: true })
    await advance(INTERVAL * 5)
    expect(view.result.current.index).toBe(0)
    await view.set({ held: false })
    await advance(1990)
    expect(view.result.current.index).toBe(0)
    await advance(10)
    expect(view.result.current.index).toBe(1)
    await view.unmount()
  })

  it('手动选择后从头算；减弱动态效果时完全不自动切', async () => {
    const view = await mountRotation()
    await advance(INTERVAL - 100)
    await act(async () => view.result.current.select(2))
    await advance(INTERVAL - 1)
    expect(view.result.current.index).toBe(2)
    await advance(1)
    expect(view.result.current.index).toBe(0)
    await view.unmount()

    const reduced = await mountRotation({ reduced: true })
    await advance(INTERVAL * 3)
    expect(reduced.result.current.index).toBe(0)
    await act(async () => reduced.result.current.select(1))
    expect(reduced.result.current.index).toBe(1)
    expect(reduced.result.current.running).toBe(false)
    await reduced.unmount()
  })

  it('标签页在后台时不切；回来后当前这张重新给满一整段', async () => {
    const view = await mountRotation()
    await advance(INTERVAL - 1000)
    await act(async () => setVisibility('hidden'))
    await advance(INTERVAL * 3)
    expect(view.result.current.index).toBe(0)
    await act(async () => setVisibility('visible'))
    await advance(INTERVAL - 1)
    expect(view.result.current.index).toBe(0)
    await advance(1)
    expect(view.result.current.index).toBe(1)
    await view.unmount()
  })
})
