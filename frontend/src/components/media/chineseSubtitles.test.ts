import { describe, expect, it } from 'vitest'
import type { SearchItem, SourceCandidate } from '../../lib/endpoints'
import { candidateChineseScore, releaseChineseScore } from './chineseSubtitles'
import { sortCandidates } from './SourcePlaybackContext'

const candidate = (id: string, metadata: SourceCandidate['metadata'], extra: Partial<SourceCandidate> = {}): SourceCandidate => ({
  schema: 'nagare-candidate/v1', id, sourceId: 'garden', tier: 3, matchConfidence: 0.9, match: { basis: ['title'] },
  transport: { type: 'torrent', infoHash: id.padStart(40, '0') }, metadata, ...extra,
})
const release = (title: string, extra: Partial<SearchItem> = {}): SearchItem => ({
  title, magnet: 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567', size: '', fansub: null, date: null, source: 'local', ...extra,
})

describe('中文字幕识别', () => {
  it('来源报了字幕语言就以它为准：有中文为 2，报了却没有中文为 0', () => {
    expect(candidateChineseScore(candidate('a', { subtitleLanguages: ['zh-Hans'] }))).toBe(2)
    expect(candidateChineseScore(candidate('b', { subtitleLanguages: ['ja', 'zh-Hant'] }))).toBe(2)
    expect(candidateChineseScore(candidate('c', { subtitleLanguages: ['简中'] }))).toBe(2)
    expect(candidateChineseScore(candidate('d', { subtitleLanguages: ['en'], title: '[X] Show - 01 [CHS]' }))).toBe(0)
  })

  it('没报字幕语言（或报了空列表）就看发布标题与字幕组名；什么都没写的算说不准', () => {
    expect(candidateChineseScore(candidate('a', { subtitleLanguages: [], title: '[LoliHouse] 葬送的芙莉莲 - 01 [WebRip 1080p][简繁内封字幕]' }))).toBe(2)
    expect(candidateChineseScore(candidate('b', { title: '[ANi] Frieren - 01 [1080P][Baha][WEB-DL][AAC AVC][CHT][MP4]' }))).toBe(2)
    expect(candidateChineseScore(candidate('c', { fansub: '北宇治字幕组' }))).toBe(2)
    expect(candidateChineseScore(candidate('d', { title: '[SubsPlease] Sousou no Frieren - 01 (1080p) [ABCD1234].mkv' }))).toBe(1)
    expect(candidateChineseScore(candidate('e', {}))).toBe(1)
  })

  it('不把体积里的 GB、日文标题里单独的「繁」当成中文字幕', () => {
    expect(releaseChineseScore(release('[Raws] Show - 01 [1080p][1.4GB]'))).toBe(1)
    expect(releaseChineseScore(release('[Raws] 繁栄の街 - 01 [1080p]'))).toBe(1)
    expect(releaseChineseScore(release('【喵萌奶茶屋】★10月新番★[葬送的芙莉莲][01][1080p][简日双语]'))).toBe(2)
    expect(releaseChineseScore(release('[Group] Show - 01 [GB][1080p]'))).toBe(2)
  })
})

describe('候选排序：中文字幕优先，但不越过来源档位', () => {
  it('同一档里中文字幕的在前；说不准的在明确没有中文的前面', () => {
    const english = candidate('en', { subtitleLanguages: ['en'], seeders: 500 })
    const unknown = candidate('unknown', { seeders: 100 })
    const chinese = candidate('zh', { title: '[LoliHouse] Show - 01 [简繁内封字幕]', seeders: 5 })
    expect(sortCandidates([english, unknown, chinese]).map(item => item.id)).toEqual(['zh', 'unknown', 'en'])
  })

  it('在线来源的档位更高时照样排在中文字幕的 BT 前面', () => {
    const online = candidate('web', {}, { tier: 1, sourceId: 'web-a', transport: { type: 'hls', url: 'https://example.invalid/a.m3u8' } })
    const chinese = candidate('zh', { subtitleLanguages: ['zh-Hans'] })
    expect(sortCandidates([chinese, online]).map(item => item.id)).toEqual(['web', 'zh'])
  })
})
