// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { MediaSummary } from '../../components/media/types'
import { resetNearViewportForTests } from '../../components/media/artworkLoading'
import { resetArtworkCacheForTests } from '../../lib/imageCache'
import { mount } from '../../test/harness'
import { installFakeIntersectionObserver } from '../../test/intersectionObserver'
import type { FakeIntersectionObservers } from '../../test/intersectionObserver'
import { EntryRelations } from './EntryInfo'

const media: MediaSummary = {
  id: 1,
  title: '作品',
  episodes: 12,
  watched: 0,
  genres: [],
  characters: [
    { name: '甲', image: '/char/a.jpg', role: 'MAIN' },
    { name: '乙', image: '', role: 'SUPPORTING' },
  ],
}

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

describe('角色头像', () => {
  it('离视口不远就改成 eager（不等 WebKit 的 lazy）；没有头像的画「流」占位', async () => {
    const { container, unmount } = await mount(<EntryRelations media={media} />)
    const avatars = container.querySelectorAll('.entry-characters img')
    expect(avatars).toHaveLength(1)
    const avatar = avatars[0]!
    expect(avatar.getAttribute('loading')).toBe('lazy')
    expect(io.isObserved(avatar)).toBe(true)
    await act(async () => io.intersect(avatar))
    expect(avatar.getAttribute('loading')).toBe('eager')
    expect(container.querySelector('.entry-character-blank')?.textContent).toBe('流')
    await unmount()
  })
})
