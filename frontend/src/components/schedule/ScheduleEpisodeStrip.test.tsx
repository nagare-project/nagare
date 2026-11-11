// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { installFakeIntersectionObserver } from '../../test/intersectionObserver'
import type { FakeIntersectionObservers } from '../../test/intersectionObserver'
import { ROW_NEAR_MARGIN, preloadEnd, resetNearViewportForTests } from '../media/artworkLoading'
import { ScheduleEpisodeStrip } from './ScheduleEpisodeStrip'
import type { ScheduleEpisode } from './types'

vi.mock('../../lib/media', async (importOriginal) => ({ ...await importOriginal<typeof import('../../lib/media')>(), fetchMedia: vi.fn(async () => { throw new Error('离线') }) }))

const COUNT = 30
const now = new Date('2026-11-10T12:00:00Z')
const episode = (i: number): ScheduleEpisode => ({
  event: {
    id: `e${i}`,
    media: { id: i, title: `作品 ${i}`, episodes: 12, watched: 0, genres: [], cover: `/art/cover-${i}` },
    episode: 3,
    airingAt: new Date(now.getTime() + (i + 1) * 3600_000).toISOString(),
    status: 'watching',
    watched: false,
    finale: false,
  },
})
const loadings = (container: HTMLElement) => [...container.querySelectorAll('img')].map((img) => img.getAttribute('loading'))

let io: FakeIntersectionObservers
beforeEach(() => {
  resetNearViewportForTests()
  io = installFakeIntersectionObserver()
})
afterEach(() => {
  resetNearViewportForTests()
  io.restore()
})

describe('放送表「即将播出」的图片加载', () => {
  it('这条离视口还远时都等着；到了附近（上下 1.5 屏）按「这一页 + 后面两页」先加载（观察器看不见被横向列表裁掉的卡）', async () => {
    const { container, unmount } = await mount(<ScheduleEpisodeStrip title="即将播出" items={Array.from({ length: COUNT }, (_, i) => episode(i))} now={now} />)
    expect(loadings(container).every((value) => value === 'lazy')).toBe(true)
    const section = container.querySelector('section')!
    expect(io.observers.find((record) => record.targets.has(section))?.options?.rootMargin).toBe(ROW_NEAR_MARGIN)
    await act(async () => io.intersect(section))
    // jsdom 量不出宽度：按默认页大小算窗口
    const end = preloadEnd([])
    expect(loadings(container).slice(0, end).every((value) => value === 'eager')).toBe(true)
    expect(loadings(container).slice(end).every((value) => value === 'lazy')).toBe(true)
    await unmount()
  })
})
