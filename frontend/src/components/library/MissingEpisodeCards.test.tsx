// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '../../test/harness'
import { MissingEpisodeCards } from './MissingEpisodeCards'
import { episodeHasAired, nextEpisodeAiring } from '../media/episode-metadata'
import type { MediaSummary } from '../media/types'
import type { LibraryCluster } from '../../lib/endpoints'

const media: MediaSummary = { id: 1, title: '作品', episodes: 3, watched: 0, genres: [], status: 'RELEASING', episodeTitles: [
  { episode: 1, title: '第一集', airDate: '2020-01-01', image: '/first.jpg' },
  { episode: 2, title: '第二集', airDate: '2020-01-08', image: '/second.jpg' },
  { episode: 3, title: '尚未播出', airDate: '2099-01-01' },
] }

describe('未入库剧集资料', () => {
  it('空库仍展示已播出的逐集资料，不把资料卡当作可播放文件', async () => {
    const { container, unmount } = await mount(<MissingEpisodeCards media={media} />)
    expect(container.querySelectorAll('.missing-episode')).toHaveLength(2)
    expect(container.querySelector('img')?.getAttribute('src')).toBe('/first.jpg')
    expect(container.textContent).toContain('2020/01/01')
    expect(container.textContent).not.toContain('尚未播出')
    expect(container.querySelector('[aria-label^="播放"]')).toBeNull()
    await unmount()
  })
  it('只排除关联版本的正片文件，同编号片头不算已入库', async () => {
    const cluster = { groups: [{ items: [{ episode: 1, kind: 'main' }, { episode: 2, kind: 'ncop' }] }] } as LibraryCluster
    const { container, unmount } = await mount(<MissingEpisodeCards media={media} cluster={cluster} />)
    expect(container.querySelectorAll('.missing-episode')).toHaveLength(1)
    expect(container.querySelector('.missing-episode')?.textContent).toContain('第二集')
    await unmount()
  })
  it('优先使用精确播出时间，倒计时回退到逐集日程', () => {
    const now = Date.parse('2026-09-13T00:00:00Z')
    const episode = { episode: 23, title: '', airDate: '2026-09-13', airedAt: '2026-09-18T14:00:00Z' }
    expect(episodeHasAired(episode, media, now)).toBe(false)
    expect(nextEpisodeAiring(media, [episode], now)).toEqual({ episode: 23, at: Date.parse(episode.airedAt) / 1000 })
    expect(nextEpisodeAiring(media, [{ ...episode, airedAt: 'invalid' }], now)).toBeUndefined()
  })
})
