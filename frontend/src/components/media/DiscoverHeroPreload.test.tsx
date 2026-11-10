// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { preloadArtwork } from '../../lib/imageCache'
import { mount } from '../../test/harness'
import { DiscoverHero } from './DiscoverHero'
import { BANNER_INTERVAL_FALLBACK_MS } from './useBannerRotation'
import type { MediaSummary } from './types'

vi.mock('../../lib/media', async (importOriginal) => ({ ...await importOriginal<typeof import('../../lib/media')>(), fetchMedia: vi.fn(async () => { throw new Error('离线') }) }))
vi.mock('../../lib/endpoints', async (importOriginal) => ({ ...await importOriginal<typeof import('../../lib/endpoints')>(), fetchLibrary: vi.fn(async () => ({ clusters: [], folders: [], scannedAt: null, continueWatching: [] })) }))
vi.mock('../../lib/imageCache', async (importOriginal) => ({ ...await importOriginal<typeof import('../../lib/imageCache')>(), preloadArtwork: vi.fn(async () => true) }))

const work = (id: number, banner = true): MediaSummary => ({
  id, title: `作品 ${id}`, episodes: 12, watched: 0, genres: [],
  cover: `/art/cover-${id}`, banner: banner ? `/art/banner-${id}` : undefined,
})
const preloaded = () => vi.mocked(preloadArtwork).mock.calls.map(([src]) => src)

beforeEach(() => {
  vi.useFakeTimers()
  vi.mocked(preloadArtwork).mockClear()
})
afterEach(() => { vi.useRealTimers() })

describe('发现页横幅：提前加载下一部', () => {
  it('显示第一部时就把第二部的背景与封面加载好；切到第二部时再加载第三部', async () => {
    const { unmount } = await mount(<DiscoverHero items={[work(1), work(2), work(3)]} label="本季新番" />)
    expect(preloaded()).toEqual(expect.arrayContaining(['/art/banner-2', '/art/cover-2']))
    expect(preloaded()).not.toContain('/art/banner-3')
    await act(async () => { vi.advanceTimersByTime(BANNER_INTERVAL_FALLBACK_MS) })
    expect(preloaded()).toEqual(expect.arrayContaining(['/art/banner-3', '/art/cover-3']))
    await unmount()
  })

  it('没有横幅图的作品，背景就是封面：只加载一次', async () => {
    const { unmount } = await mount(<DiscoverHero items={[work(1), work(2, false)]} label="本季新番" />)
    expect(preloaded().filter((src) => src === '/art/cover-2')).toHaveLength(1)
    await unmount()
  })

  it('只有一部时不预加载', async () => {
    const { unmount } = await mount(<DiscoverHero items={[work(1)]} label="本季新番" />)
    expect(preloaded()).toEqual([])
    await unmount()
  })
})
