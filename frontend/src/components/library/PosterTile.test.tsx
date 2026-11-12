// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { isArtworkLoaded, markArtworkLoaded, resetArtworkCacheForTests } from '../../lib/imageCache'
import { mount } from '../../test/harness'
import { installFakeIntersectionObserver } from '../../test/intersectionObserver'
import type { FakeIntersectionObservers } from '../../test/intersectionObserver'
import { NEAR_VIEWPORT_MARGIN, resetNearViewportForTests } from '../media/artworkLoading'
import { PosterTile } from './PosterTile'

const COVER = '/art/cap/remote/poster-1'
const img = (container: HTMLElement) => container.querySelector('img')

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

describe('媒体库海报卡的封面', () => {
  it('离视口还远时不急着加载；进到 NEAR_VIEWPORT_MARGIN 以内就改成 eager（不等 WebKit 的 lazy）', async () => {
    const { container, unmount } = await mount(<PosterTile cover={COVER} title="作品" />)
    expect(img(container)!.getAttribute('loading')).toBe('lazy')
    expect(io.isObserved(img(container)!)).toBe(true)
    expect(io.observers.some((o) => o.options?.rootMargin === NEAR_VIEWPORT_MARGIN)).toBe(true)
    await act(async () => io.intersect(img(container)!))
    expect(img(container)!.getAttribute('loading')).toBe('eager')
    await unmount()
  })

  it('还没到时透明（露出占位底色），到了淡入并记下', async () => {
    const { container, unmount } = await mount(<PosterTile cover={COVER} title="作品" />)
    expect(img(container)!.dataset.loaded).toBe('false')
    await act(async () => { img(container)!.dispatchEvent(new Event('load')) })
    expect(img(container)!.dataset.loaded).toBe('true')
    expect(isArtworkLoaded(COVER)).toBe(true)
    await unmount()
  })

  it('加载过的封面再挂载（重扫媒体库、回到媒体库）直接显示，不从透明淡入', async () => {
    markArtworkLoaded(COVER)
    const { container, unmount } = await mount(<PosterTile cover={COVER} title="作品" />)
    expect(img(container)!.dataset.loaded).toBe('cached')
    await unmount()
  })

  it('没有封面或加载失败时画「流」占位，不留空框', async () => {
    const empty = await mount(<PosterTile title="作品" />)
    expect(img(empty.container)).toBeNull()
    expect(empty.container.querySelector('.poster-tile-blank')?.textContent).toBe('流')
    await empty.unmount()

    const failing = await mount(<PosterTile cover={COVER} title="作品" />)
    await act(async () => { img(failing.container)!.dispatchEvent(new Event('error')) })
    expect(img(failing.container)).toBeNull()
    expect(failing.container.querySelector('.poster-tile-blank')).not.toBeNull()
    await failing.unmount()
  })
})
