import { describe, expect, it } from 'vitest'
import { episodeHasAired, nextEpisodeAiring } from './episode-metadata'
import type { MediaSummary } from './types'

const media: MediaSummary = { id: 1, title: '作品', episodes: 3, watched: 0, genres: [], status: 'RELEASING' }

describe('逐集播出时间', () => {
  it('优先使用精确播出时间，倒计时回退到逐集日程', () => {
    const now = Date.parse('2026-09-13T00:00:00Z')
    const episode = { episode: 23, title: '', airDate: '2026-09-13', airedAt: '2026-09-18T14:00:00Z' }
    expect(episodeHasAired(episode, media, now)).toBe(false)
    expect(nextEpisodeAiring(media, [episode], now)).toEqual({ episode: 23, at: Date.parse(episode.airedAt) / 1000 })
    expect(nextEpisodeAiring(media, [{ ...episode, airedAt: 'invalid' }], now)).toBeUndefined()
  })
})
