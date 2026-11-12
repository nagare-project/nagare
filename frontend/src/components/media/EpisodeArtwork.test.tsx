// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { markArtworkLoaded, resetArtworkCacheForTests } from '../../lib/imageCache'
import { mount } from '../../test/harness'
import { installFakeIntersectionObserver } from '../../test/intersectionObserver'
import type { FakeIntersectionObservers } from '../../test/intersectionObserver'
import { resetNearViewportForTests } from './artworkLoading'
import { EpisodeArtwork } from './EpisodeArtwork'

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

describe('逐集截图', () => {
  it('优先真实截图，失败后依次回退横图和海报', async () => {
    const { container, unmount } = await mount(<EpisodeArtwork image="/still.jpg" banner="/banner.jpg" cover="/cover.jpg" />)
    const src = () => container.querySelector('img')?.getAttribute('src')
    expect(src()).toBe('/still.jpg')
    await act(async () => container.querySelector('img')!.dispatchEvent(new Event('error')))
    expect(src()).toBe('/banner.jpg')
    await act(async () => container.querySelector('img')!.dispatchEvent(new Event('error')))
    expect(src()).toBe('/cover.jpg')
    await act(async () => container.querySelector('img')!.dispatchEvent(new Event('error')))
    expect(container.textContent).toBe('暂无图片')
    await unmount()
  })
  it('切换图片后重新尝试新地址', async () => {
    const { container, rerender, unmount } = await mount(<EpisodeArtwork image="/old.jpg" />)
    await act(async () => container.querySelector('img')!.dispatchEvent(new Event('error')))
    await rerender(<EpisodeArtwork image="/next.jpg" />)
    expect(container.querySelector('img')?.getAttribute('src')).toBe('/next.jpg')
    await unmount()
  })
  it('离视口不远就改成 eager，到了淡入；加载过的截图直接显示', async () => {
    const { container, unmount } = await mount(<EpisodeArtwork image="/still.jpg" />)
    const img = () => container.querySelector('img')!
    expect(img().getAttribute('loading')).toBe('lazy')
    expect(img().dataset.loaded).toBe('false')
    await act(async () => io.intersect(img()))
    expect(img().getAttribute('loading')).toBe('eager')
    await act(async () => { img().dispatchEvent(new Event('load')) })
    expect(img().dataset.loaded).toBe('true')
    await unmount()

    markArtworkLoaded('/cached.jpg')
    const again = await mount(<EpisodeArtwork image="/cached.jpg" />)
    expect(again.container.querySelector('img')!.dataset.loaded).toBe('cached')
    await again.unmount()
  })
})
