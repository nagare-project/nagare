// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PRELOAD_TIMEOUT_MS, isArtworkLoaded, markArtworkLoaded, preloadArtwork, resetArtworkCacheForTests } from './imageCache'

/** 可以手动报 load / error 的 Image 替身 */
class FakeImage {
  static created: FakeImage[] = []
  static decodeResult: 'ok' | 'reject' = 'ok'
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  src = ''
  decode = vi.fn(() => (FakeImage.decodeResult === 'ok' ? Promise.resolve() : Promise.reject(new Error('EncodingError'))))
  constructor() { FakeImage.created.push(this) }
}

const original = globalThis.Image
beforeEach(() => {
  resetArtworkCacheForTests()
  FakeImage.created = []
  FakeImage.decodeResult = 'ok'
  globalThis.Image = FakeImage as unknown as typeof Image
})
afterEach(() => {
  globalThis.Image = original
  vi.useRealTimers()
})

describe('preloadArtwork', () => {
  it('加载并解码完才算好，好了就记下；在途时同一个地址只请求一次', async () => {
    const first = preloadArtwork('/a')
    const second = preloadArtwork('/a')
    expect(second).toBe(first)
    expect(FakeImage.created).toHaveLength(1)
    expect(FakeImage.created[0]!.src).toBe('/a')
    expect(isArtworkLoaded('/a')).toBe(false)
    FakeImage.created[0]!.onload?.()
    await expect(first).resolves.toBe(true)
    expect(FakeImage.created[0]!.decode).toHaveBeenCalled()
    expect(isArtworkLoaded('/a')).toBe(true)
    // 记下之后不再发请求
    await expect(preloadArtwork('/a')).resolves.toBe(true)
    expect(FakeImage.created).toHaveLength(1)
  })

  it('解码被拒（个别图 Safari 会拒）仍算加载成功', async () => {
    FakeImage.decodeResult = 'reject'
    const request = preloadArtwork('/b')
    FakeImage.created[0]!.onload?.()
    await expect(request).resolves.toBe(true)
  })

  it('请求或解码卡住：最多等 PRELOAD_TIMEOUT_MS 就按失败返回，在途记录清掉、下次重新请求', async () => {
    vi.useFakeTimers()
    const stalled = preloadArtwork('/stuck')
    let settled: boolean | undefined
    void stalled.then((ok) => { settled = ok })
    await vi.advanceTimersByTimeAsync(PRELOAD_TIMEOUT_MS - 1)
    expect(settled).toBeUndefined()
    await vi.advanceTimersByTimeAsync(1)
    expect(settled).toBe(false)
    expect(isArtworkLoaded('/stuck')).toBe(false)
    expect(preloadArtwork('/stuck')).not.toBe(stalled)
    expect(FakeImage.created).toHaveLength(2)
  })

  it('解码一直不回（onload 之后卡在 decode）同样按超时处理', async () => {
    vi.useFakeTimers()
    const request = preloadArtwork('/decode-stuck')
    FakeImage.created[0]!.decode.mockImplementation(() => new Promise<void>(() => {}))
    FakeImage.created[0]!.onload?.()
    await vi.advanceTimersByTimeAsync(PRELOAD_TIMEOUT_MS)
    await expect(request).resolves.toBe(false)
  })

  it('加载失败返回 false、不记下，下次再试会重新请求', async () => {
    const request = preloadArtwork('/c')
    FakeImage.created[0]!.onerror?.()
    await expect(request).resolves.toBe(false)
    expect(isArtworkLoaded('/c')).toBe(false)
    void preloadArtwork('/c')
    expect(FakeImage.created).toHaveLength(2)
  })
})

describe('加载过的地址', () => {
  it('有上限：超出时丢掉最久没用到的', () => {
    markArtworkLoaded('/oldest')
    markArtworkLoaded('/kept')
    for (let i = 0; i < 4095; i++) {
      markArtworkLoaded(`/fill-${i}`)
      // 一直在用的那张会被挪到最后，不会被丢
      if (i === 4000) markArtworkLoaded('/kept')
    }
    expect(isArtworkLoaded('/oldest')).toBe(false)
    expect(isArtworkLoaded('/kept')).toBe(true)
    expect(isArtworkLoaded('/fill-4094')).toBe(true)
  })

  it('查一次也算用到：只被查到（重新挂载时命中缓存）的封面不会被后来加载的挤掉', () => {
    markArtworkLoaded('/oldest')
    markArtworkLoaded('/read')
    // 再记 4096 个：超出两个。只记不查的话被丢的是 /oldest 与 /read
    for (let i = 0; i < 4096; i++) {
      markArtworkLoaded(`/fill-${i}`)
      if (i === 4000) expect(isArtworkLoaded('/read')).toBe(true)
    }
    expect(isArtworkLoaded('/oldest')).toBe(false)
    expect(isArtworkLoaded('/fill-0')).toBe(false)
    expect(isArtworkLoaded('/read')).toBe(true)
  })

  it('没加载过的地址查了也不会被记下', () => {
    expect(isArtworkLoaded('/never')).toBe(false)
    expect(isArtworkLoaded('/never')).toBe(false)
  })
})
