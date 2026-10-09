import { describe, expect, it } from 'vitest'
import type { SearchItem } from '../../lib/endpoints'
import { catalogIdentity, defaultTorrentEpisode, episodeChoices, episodeFit, extraKindsFor, isBatchRelease, notBeforeFor, playbackHints, releasesForEpisode } from './releaseEpisodes'
import type { EpisodeTarget } from './releaseEpisodes'
import type { MediaSummary } from './types'

const item = (extra: Partial<SearchItem>): SearchItem => ({
  title: '[G] 某作品 - 03 [1080p]', magnet: 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567',
  size: '', fansub: null, date: null, source: 'local', kind: 'main', ...extra,
})
const media = (extra: Partial<MediaSummary>): MediaSummary => ({ id: 1, title: '某作品', episodes: 12, watched: 0, genres: [], status: 'FINISHED', ...extra })
const third: EpisodeTarget = { episode: 3, season: 1 }
const sequelThird: EpisodeTarget = { episode: 3, season: 2, offset: 12 }

describe('episodeFit', () => {
  it('单集按集号对上；特典、别的集不算', () => {
    expect(episodeFit(item({ episode: 3 }), third)).toBe('exact')
    expect(episodeFit(item({ episode: 4 }), third)).toBe('other')
    expect(episodeFit(item({ episode: 3, kind: 'sp' }), third)).toBe('other')
    expect(episodeFit(item({ episode: undefined, title: '[G] 某作品 PV' }), third)).toBe('other')
  })

  it('标题写明别的季就不是这一部；没写季数的不排除', () => {
    expect(episodeFit(item({ episode: 3, season: 4 }), third)).toBe('other')
    expect(episodeFit(item({ episode: 3, season: 2 }), sequelThird)).toBe('exact')
    expect(episodeFit(item({ episode: 3 }), sequelThird)).toBe('exact')
  })

  it('作品是第几季说不准时不按季数排除；OVA、特别篇作品的 OVA/SP 发布算这一集', () => {
    const unknownSeason: EpisodeTarget = { episode: 3, offset: 26 }
    expect(episodeFit(item({ episode: 3, season: 2 }), unknownSeason)).toBe('exact')
    expect(episodeFit(item({ episode: 29, season: 2 }), unknownSeason)).toBe('offset')
    const ova: EpisodeTarget = { episode: 2, season: 1, extraKinds: extraKindsFor('OVA') }
    expect(episodeFit(item({ episode: 2, kind: 'ova' }), ova)).toBe('exact')
    expect(episodeFit(item({ episode: 2, kind: 'ncop' }), ova)).toBe('other')
    expect(playbackHints(item({ episode: 2, kind: 'ova' }), ova)).toEqual({ episodeHint: 2 })
    expect(extraKindsFor('TV')).toBeUndefined()
    expect(extraKindsFor(undefined)).toBeUndefined()
  })

  it('跨季连续编号：第二季第 3 集在标题里叫 15', () => {
    expect(episodeFit(item({ episode: 15, season: 2 }), sequelThird)).toBe('offset')
    expect(episodeFit(item({ episode: 15 }), third)).toBe('other')
  })

  it('合集按写明的区间判断，没写区间的说不准', () => {
    const batch = (low: number, high: number) => item({ kind: 'batch', episode: undefined, episodeRange: { low, high }, title: `[G] 某作品 [${low}-${high}]` })
    expect(episodeFit(batch(1, 12), third)).toBe('batch')
    expect(episodeFit(batch(4, 12), third)).toBe('other')
    expect(episodeFit(batch(13, 24), sequelThird)).toBe('batch')
    expect(episodeFit(item({ kind: 'batch', episode: undefined, title: '[G] 某作品 合集' }), third)).toBe('batch-unknown')
  })

  it('releasesForEpisode 只留这一集与可能含它的合集', () => {
    const list = [item({ episode: 3 }), item({ episode: 4 }), item({ kind: 'batch', episode: undefined, title: '合集' })]
    expect(releasesForEpisode(list, third)).toEqual([list[0], list[2]])
  })
})

describe('playbackHints', () => {
  it('单集按条目自己的集号；连续编号的「15」种子里文件也叫 15', () => {
    expect(playbackHints(item({ episode: 3 }), third)).toEqual({ episodeHint: 3 })
    expect(playbackHints(item({ episode: 15 }), sequelThird)).toEqual({ episodeHint: 15 })
    expect(playbackHints(item({ episode: 1, kind: 'sp' }), third)).toEqual({})
    expect(playbackHints(item({ episode: undefined, kind: 'main', title: '[G] 某作品' }), third)).toEqual({})
    // 超出后端接受范围的「集号」（日期一类）不当提示发出去，否则整个播放请求被拒
    expect(playbackHints(item({ episode: 20250301 }), third)).toEqual({})
  })

  it('合集按选中的集号；连续编号时按区间选一种，说不准就两种都给', () => {
    const batch = (range?: { low: number; high: number }) => item({ kind: 'batch', episode: undefined, ...(range ? { episodeRange: range } : {}) })
    expect(playbackHints(batch({ low: 1, high: 28 }), third)).toEqual({ episodeHint: 3 })
    expect(playbackHints(batch({ low: 13, high: 24 }), sequelThird)).toEqual({ episodeHint: 15 })
    expect(playbackHints(batch({ low: 1, high: 12 }), sequelThird)).toEqual({ episodeHint: 3 })
    expect(playbackHints(batch({ low: 1, high: 24 }), sequelThird)).toEqual({ episodeHint: 3, altEpisodeHint: 15 })
    expect(playbackHints(batch(), sequelThird)).toEqual({ episodeHint: 3, altEpisodeHint: 15 })
    // 剧场版不按集定位
    expect(playbackHints(batch({ low: 1, high: 28 }), null)).toEqual({})
  })
})

describe('集数选择', () => {
  it('默认下一集没看的；看完停在最后一集；没登录从第 1 集起', () => {
    expect(defaultTorrentEpisode(media({ watched: 0 }))).toBe(1)
    expect(defaultTorrentEpisode(media({ watched: 4 }))).toBe(5)
    expect(defaultTorrentEpisode(media({ watched: 12 }))).toBe(12)
    expect(defaultTorrentEpisode(media({ episodes: null, status: 'NOT_YET_RELEASED', watched: 0 }))).toBe(1)
  })

  it('集号按钮：不多时全列，长篇只列进度附近一段', () => {
    expect(episodeChoices(media({}))).toEqual(Array.from({ length: 12 }, (_, i) => i + 1))
    const long = episodeChoices(media({ episodes: 1100, watched: 500 }), 120)
    expect(long).toHaveLength(120)
    expect(long[0]).toBe(490)
    expect(long).toContain(501)
  })
})

describe('isBatchRelease / catalogIdentity', () => {
  it('后端认出的合集为准；没集号又带 BD 字样的也按合集', () => {
    expect(isBatchRelease(item({ kind: 'batch', episode: undefined }))).toBe(true)
    expect(isBatchRelease(item({ episode: undefined, title: '[G] 某作品 [BDRip 1080p]' }))).toBe(true)
    expect(isBatchRelease(item({ episode: 3, title: '[G] 某作品 [03][BDRip 1080p]' }))).toBe(false)
  })

  it('作品身份：去空白、不分大小写去重', () => {
    expect(catalogIdentity({ id: 9, title: ' 葬送的芙莉莲 ', titleNative: 'Frieren', titleEnglish: 'frieren' })).toEqual({ anilistId: 9, titles: ['葬送的芙莉莲', 'Frieren'] })
    expect(catalogIdentity({ id: 9, title: '某作品' })).toEqual({ anilistId: 9, titles: ['某作品'] })
  })
})

describe('续作页上的前作整季包', () => {
  const pack = (low: number, high: number, title = `[G] Show [${low}-${high}]`) => ({ title, magnet: 'magnet:?xt=urn:btih:a', kind: 'batch', episode: null, episodeRange: { low, high } }) as unknown as SearchItem
  const s2 = { episode: 1, offset: 28, total: 10 }

  it('区间伸出本季集数、又不含连续编号的这一集：是前作的包，不挂在这一集下面', () => {
    expect(episodeFit(pack(1, 28), s2)).toBe('other')
  })

  it('本季自己编号的包、连续编号的包、前作加本季的大包都还在', () => {
    expect(episodeFit(pack(1, 10), s2)).toBe('batch')
    expect(episodeFit(pack(29, 38), s2)).toBe('batch')
    expect(episodeFit(pack(1, 38), s2)).toBe('batch')
  })

  it('不知道有没有前作时照旧按区间判断', () => {
    expect(episodeFit(pack(1, 28), { episode: 1, total: 10 })).toBe('batch')
  })
})

describe('发布日期早于这一季开播', () => {
  const single = (date: string | null) => ({ title: '[G] Sousou no Frieren - 01 [BDRip]', magnet: 'magnet:?xt=urn:btih:a', episode: 1, kind: 'main', date }) as unknown as SearchItem
  const target = { episode: 1, offset: 28, notBefore: notBeforeFor('2026-01-16') }

  it('前作的单集（标题没写季数）不挂在续作的这一集下面', () => {
    expect(episodeFit(single('2024-11-02T10:00:00Z'), target)).toBe('other')
  })

  it('开播前不久（先行放送、时区差）与开播之后的照常算', () => {
    expect(episodeFit(single('2026-01-01T10:00:00Z'), target)).toBe('exact')
    expect(episodeFit(single('2026-01-20T10:00:00Z'), target)).toBe('exact')
  })

  it('没有发布日期或不知道首播日期时不按日期排除', () => {
    expect(episodeFit(single(null), target)).toBe('exact')
    expect(notBeforeFor(undefined)).toBeUndefined()
    expect(notBeforeFor('不是日期')).toBeUndefined()
  })
})

