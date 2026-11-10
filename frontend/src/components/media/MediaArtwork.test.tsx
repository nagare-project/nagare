// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { isArtworkLoaded, markArtworkLoaded, resetArtworkCacheForTests } from '../../lib/imageCache'
import { mount } from '../../test/harness'
import { installFakeIntersectionObserver } from '../../test/intersectionObserver'
import type { FakeIntersectionObservers } from '../../test/intersectionObserver'
import { ArtworkLoadingContext, NEAR_VIEWPORT_MARGIN, resetNearViewportForTests } from './artworkLoading'
import { MediaArtwork } from './MediaArtwork'

const SRC = '/art/cap/remote/cover-1'
const img = (container: HTMLElement) => container.querySelector('img')!

let io: FakeIntersectionObservers
beforeEach(() => {
  resetArtworkCacheForTests()
  resetNearViewportForTests()
  io = installFakeIntersectionObserver()
})
afterEach(() => {
  resetNearViewportForTests()
  io.restore()
})

describe('封面淡入（reveal）', () => {
  it('还没加载的图先透明，加载完才淡入；加载过就记下来', async () => {
    const { container, unmount } = await mount(<MediaArtwork src={SRC} title="作品" reveal />)
    expect(img(container).dataset.loaded).toBe('false')
    await act(async () => { img(container).dispatchEvent(new Event('load')) })
    expect(img(container).dataset.loaded).toBe('true')
    expect(isArtworkLoaded(SRC)).toBe(true)
    await unmount()
  })

  it('本页面里加载过的图再挂载一次（换筛选、回到发现页）直接显示，不从透明淡入', async () => {
    markArtworkLoaded(SRC)
    const { container, unmount } = await mount(<MediaArtwork src={SRC} title="作品" reveal />)
    expect(img(container).dataset.loaded).toBe('cached')
    await unmount()
  })

  it('挂载那一刻图已经 complete（浏览器缓存命中）也直接显示', async () => {
    const complete = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, 'complete')
    const naturalWidth = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, 'naturalWidth')
    Object.defineProperty(HTMLImageElement.prototype, 'complete', { configurable: true, get: () => true })
    Object.defineProperty(HTMLImageElement.prototype, 'naturalWidth', { configurable: true, get: () => 460 })
    try {
      const { container, unmount } = await mount(<MediaArtwork src={SRC} title="作品" reveal />)
      expect(img(container).dataset.loaded).toBe('cached')
      await unmount()
    } finally {
      if (complete) Object.defineProperty(HTMLImageElement.prototype, 'complete', complete)
      if (naturalWidth) Object.defineProperty(HTMLImageElement.prototype, 'naturalWidth', naturalWidth)
    }
  })

  it('慢慢加载完的图不会中途改成「直接显示」（那会打断淡入）', async () => {
    const { container, unmount } = await mount(<MediaArtwork src={SRC} title="作品" reveal />)
    await act(async () => { img(container).dispatchEvent(new Event('load')) })
    expect(img(container).dataset.loaded).toBe('true')
    await unmount()
  })

  it('不带 reveal 的图没有 data-loaded', async () => {
    const { container, unmount } = await mount(<MediaArtwork src={SRC} title="作品" />)
    expect(img(container).hasAttribute('data-loaded')).toBe(false)
    await unmount()
  })
})

describe('封面什么时候开始加载', () => {
  it('eager 立刻加载，不挂观察器', async () => {
    const { container, unmount } = await mount(<MediaArtwork src={SRC} title="作品" eager />)
    expect(img(container).getAttribute('loading')).toBe('eager')
    expect(io.isObserved(img(container))).toBe(false)
    await unmount()
  })

  it('在轮播行里听行的：行给 eager 就加载，给 lazy 就等着（不自己观察，被行视口裁掉的卡观察器看不见）', async () => {
    const eager = await mount(<ArtworkLoadingContext.Provider value="eager"><MediaArtwork src={SRC} title="作品" /></ArtworkLoadingContext.Provider>)
    expect(img(eager.container).getAttribute('loading')).toBe('eager')
    await eager.unmount()
    const lazy = await mount(<ArtworkLoadingContext.Provider value="lazy"><MediaArtwork src={SRC} title="作品" /></ArtworkLoadingContext.Provider>)
    expect(img(lazy.container).getAttribute('loading')).toBe('lazy')
    expect(io.isObserved(img(lazy.container))).toBe(false)
    await lazy.unmount()
  })

  it('不在轮播行里：离视口 1200px 以内才改成 eager（WebKit 自带的懒加载要到快露出来才加载）', async () => {
    const { container, unmount } = await mount(<MediaArtwork src={SRC} title="作品" />)
    expect(img(container).getAttribute('loading')).toBe('lazy')
    expect(io.observers.find((record) => record.targets.has(img(container)))?.options?.rootMargin).toBe(NEAR_VIEWPORT_MARGIN)
    await act(async () => io.intersect(img(container)))
    expect(img(container).getAttribute('loading')).toBe('eager')
    expect(io.isObserved(img(container))).toBe(false)
    await unmount()
  })

  it('同一个距离的观察器全页共用一个', async () => {
    const a = await mount(<MediaArtwork src={SRC} title="作品" />)
    const b = await mount(<MediaArtwork src={`${SRC}-2`} title="作品" />)
    expect(io.observers.filter((record) => record.options?.rootMargin === NEAR_VIEWPORT_MARGIN)).toHaveLength(1)
    await a.unmount()
    await b.unmount()
  })
})
