import { describe, expect, it } from 'vitest'
import type { SearchItem } from '../../lib/endpoints'
import { catalogIdentity, defaultTorrentEpisode, extraKindsFor, isBatchRelease, playbackHints } from './releaseEpisodes'
import type { MediaSummary } from './types'

const item = (extra: Partial<SearchItem>): SearchItem => ({
  title: '[G] 某作品 - 03 [1080p]', magnet: 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567',
  size: '', fansub: null, date: null, source: 'local', kind: 'main', ...extra,
})
const media = (extra: Partial<MediaSummary>): MediaSummary => ({ id: 1, title: '某作品', episodes: 12, watched: 0, genres: [], status: 'FINISHED', ...extra })

describe('playbackHints', () => {
  it('单集按条目自己的集号；连续编号的「15」种子里文件也叫 15', () => {
    expect(playbackHints(item({ episode: 3 }))).toEqual({ episodeHint: 3 })
    expect(playbackHints(item({ episode: 15, season: 2 }))).toEqual({ episodeHint: 15 })
    expect(playbackHints(item({ episode: undefined, kind: 'main', title: '[G] 某作品' }))).toEqual({})
    // 超出后端接受范围的「集号」（日期一类）不当提示发出去，否则整个播放请求被拒
    expect(playbackHints(item({ episode: 20250301 }))).toEqual({})
  })

  it('SP、OVA 不当正片的集号；OVA、特别篇作品自己的 OVA/SP 发布照常定位', () => {
    expect(playbackHints(item({ episode: 1, kind: 'sp' }))).toEqual({})
    expect(playbackHints(item({ episode: 2, kind: 'ova' }), extraKindsFor('OVA'))).toEqual({ episodeHint: 2 })
    expect(playbackHints(item({ episode: 2, kind: 'ncop' }), extraKindsFor('OVA'))).toEqual({})
    expect(extraKindsFor('TV')).toBeUndefined()
    expect(extraKindsFor(undefined)).toBeUndefined()
  })

  it('合集不带集号：合集标题里的数字不是集号，播放前由用户在文件列表里挑', () => {
    expect(playbackHints(item({ kind: 'batch', episode: undefined, episodeRange: { low: 1, high: 28 } }))).toEqual({})
    expect(playbackHints(item({ episode: undefined, title: '[G] 某作品 [BDRip 1080p]' }))).toEqual({})
  })
})

describe('老插件用的集号', () => {
  it('默认下一集没看的；看完停在最后一集；没登录从第 1 集起', () => {
    expect(defaultTorrentEpisode(media({ watched: 0 }))).toBe(1)
    expect(defaultTorrentEpisode(media({ watched: 4 }))).toBe(5)
    expect(defaultTorrentEpisode(media({ watched: 12 }))).toBe(12)
    expect(defaultTorrentEpisode(media({ episodes: null, status: 'NOT_YET_RELEASED', watched: 0 }))).toBe(1)
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

  it('作品身份带上罗马音：只用罗马音登记的发布（nyaa、部分字幕组）也搜得到', () => {
    expect(catalogIdentity({ id: 188525, title: '描绘直至生命尽头', titleRomaji: 'Kore Kaite Shine', titleNative: 'これ描いて死ね', titleEnglish: 'Draw This, Then Die!' }))
      .toEqual({ anilistId: 188525, titles: ['描绘直至生命尽头', 'Kore Kaite Shine', 'これ描いて死ね', 'Draw This, Then Die!'] })
    // 没有中文名时显示标题就是罗马音，不重复
    expect(catalogIdentity({ id: 1, title: 'Kore Kaite Shine', titleRomaji: 'kore kaite shine' })).toEqual({ anilistId: 1, titles: ['Kore Kaite Shine'] })
  })
})
