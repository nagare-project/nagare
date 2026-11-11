// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { HERO_FADE_FALLBACK_MS, HeroBackdrop } from './HeroBackdrop'

// 预加载替身：每个地址一个手动兑现的 promise，「哪张图到了」由用例决定
const { preload } = vi.hoisted(() => {
  const pending = new Map<string, (ok: boolean) => void>()
  const loaded = new Set<string>()
  return {
    preload: {
      pending,
      loaded,
      preloadArtwork: (src: string) => new Promise<boolean>((resolve) => { pending.set(src, resolve) }),
    },
  }
})
vi.mock('../../lib/imageCache', () => ({
  preloadArtwork: preload.preloadArtwork,
  isArtworkLoaded: (src: string) => preload.loaded.has(src),
  markArtworkLoaded: (src: string) => { preload.loaded.add(src) },
}))

const layers = (container: HTMLElement) => [...container.querySelectorAll<HTMLElement>('.hero-backdrop-image')]
const layerSrcs = (container: HTMLElement) => layers(container).map((layer) => layer.querySelector('img')?.getAttribute('src'))
async function arrive(src: string, ok = true) {
  await act(async () => { preload.pending.get(src)?.(ok) })
}

beforeEach(() => {
  preload.pending.clear()
  preload.loaded.clear()
})
afterEach(() => { vi.useRealTimers() })

describe('横幅背景切换', () => {
  it('第一张直接挂上（之前没有图可留）；没加载过的淡入，加载过的（回到发现页）直接显示', async () => {
    const fresh = await mount(<HeroBackdrop src="/a" title="A" reduced={false} />)
    expect(layerSrcs(fresh.container)).toEqual(['/a'])
    expect(layers(fresh.container)[0]!.hasAttribute('data-instant')).toBe(false)
    await fresh.unmount()
    preload.loaded.add('/a')
    const cached = await mount(<HeroBackdrop src="/a" title="A" reduced={false} />)
    expect(layers(cached.container)[0]!.hasAttribute('data-instant')).toBe(true)
    await cached.unmount()
  })

  it('新图没到之前旧图一直在（不露底色）；到了才盖上去淡入，淡入完收掉旧图', async () => {
    const { container, rerender, unmount } = await mount(<HeroBackdrop src="/a" title="A" reduced={false} />)
    await rerender(<HeroBackdrop src="/b" title="B" reduced={false} />)
    expect(layerSrcs(container)).toEqual(['/a'])
    await arrive('/b')
    expect(layerSrcs(container)).toEqual(['/a', '/b'])
    expect(layers(container)[1]!.hasAttribute('data-instant')).toBe(false)
    // jsdom 没有 AnimationEvent，React 改听带前缀的 webkitAnimationEnd：两个都发
    await act(async () => {
      for (const type of ['animationend', 'webkitAnimationEnd']) layers(container)[1]!.dispatchEvent(new Event(type, { bubbles: true }))
    })
    expect(layerSrcs(container)).toEqual(['/b'])
    await unmount()
  })

  it('animationend 没来（标签页在后台）也会在兜底时间后收掉旧图', async () => {
    vi.useFakeTimers()
    const { container, rerender, unmount } = await mount(<HeroBackdrop src="/a" title="A" reduced={false} />)
    await rerender(<HeroBackdrop src="/b" title="B" reduced={false} />)
    await arrive('/b')
    await act(async () => { vi.advanceTimersByTime(HERO_FADE_FALLBACK_MS - 1) })
    expect(layerSrcs(container)).toEqual(['/a', '/b'])
    await act(async () => { vi.advanceTimersByTime(1) })
    expect(layerSrcs(container)).toEqual(['/b'])
    await unmount()
  })

  it('减弱动态效果：没有淡入；新图解码完、画上屏之后才收掉旧图 —— 同样不露底色', async () => {
    vi.useFakeTimers()
    // 新挂上的 <img> 的 decode() 由用例决定什么时候完成
    let decoded: () => void = () => {}
    const decode = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, 'decode')
    Object.defineProperty(HTMLImageElement.prototype, 'decode', { configurable: true, value: () => new Promise<void>((resolve) => { decoded = resolve }) })
    try {
      const { container, rerender, unmount } = await mount(<HeroBackdrop src="/a" title="A" reduced />)
      await rerender(<HeroBackdrop src="/b" title="B" reduced />)
      expect(layerSrcs(container)).toEqual(['/a'])
      await arrive('/b')
      await act(async () => { await vi.advanceTimersByTimeAsync(100) })
      expect(layerSrcs(container)).toEqual(['/a', '/b'])
      await act(async () => {
        decoded()
        await vi.advanceTimersByTimeAsync(100)
      })
      expect(layerSrcs(container)).toEqual(['/b'])
      await unmount()
    } finally {
      if (decode) Object.defineProperty(HTMLImageElement.prototype, 'decode', decode)
      else Reflect.deleteProperty(HTMLImageElement.prototype, 'decode')
    }
  })

  it('切走又切回来（A → B → A）：B 晚到了也不会盖上来', async () => {
    const { container, rerender, unmount } = await mount(<HeroBackdrop src="/a" title="A" reduced={false} />)
    await rerender(<HeroBackdrop src="/b" title="B" reduced={false} />)
    await rerender(<HeroBackdrop src="/a" title="A" reduced={false} />)
    await arrive('/b')
    expect(layerSrcs(container)).toEqual(['/a'])
    await unmount()
  })

  it('等新图期间横幅被卸掉（离开发现页）：图晚到了不报错、不更新', async () => {
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      const { rerender, unmount } = await mount(<HeroBackdrop src="/a" title="A" reduced={false} />)
      await rerender(<HeroBackdrop src="/b" title="B" reduced={false} />)
      await unmount()
      await arrive('/b')
      expect(errors).not.toHaveBeenCalled()
    } finally {
      errors.mockRestore()
    }
  })

  it('连着切了几次：只认最后一张，先前没到的图晚到了也不会盖上来', async () => {
    const { container, rerender, unmount } = await mount(<HeroBackdrop src="/a" title="A" reduced={false} />)
    await rerender(<HeroBackdrop src="/b" title="B" reduced={false} />)
    await rerender(<HeroBackdrop src="/c" title="C" reduced={false} />)
    await arrive('/c')
    expect(layerSrcs(container)).toEqual(['/a', '/c'])
    await arrive('/b')
    expect(layerSrcs(container)).toEqual(['/a', '/c'])
    await unmount()
  })

  it('新图加载失败也换上（不能拿上一部的图配这一部的标题）', async () => {
    const { container, rerender, unmount } = await mount(<HeroBackdrop src="/a" title="A" reduced={false} />)
    await rerender(<HeroBackdrop src="/b" title="B" reduced={false} />)
    await arrive('/b', false)
    expect(layerSrcs(container)).toEqual(['/a', '/b'])
    await unmount()
  })

  it('这一部没有图：清掉上一部的图；再换到有图的作品时照常淡入', async () => {
    const { container, rerender, unmount } = await mount(<HeroBackdrop src="/a" title="A" reduced={false} />)
    await rerender(<HeroBackdrop src={undefined} title="B" reduced={false} />)
    expect(layers(container)).toHaveLength(0)
    await rerender(<HeroBackdrop src="/c" title="C" reduced={false} />)
    await arrive('/c')
    expect(layerSrcs(container)).toEqual(['/c'])
    expect(layers(container)[0]!.hasAttribute('data-instant')).toBe(false)
    await unmount()
  })
})
