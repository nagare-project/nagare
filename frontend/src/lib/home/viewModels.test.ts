import { describe, expect, test } from 'vitest'
import type { HomeAiringData, HomeMediaData, RankedMediaData } from '../endpoints'
import {
  airingsOf, bangumiScoreText, episodesText, gemCard, heroSlide, hueCard, scoreText, seasonCard, todayCard, trendCard, yearCard,
} from './viewModels'

const row = (over: Partial<HomeMediaData> = {}): HomeMediaData => ({
  anilistId: 7, title: '葬送的芙莉莲', titleNative: '葬送のフリーレン', cover: '/art/c', hue: 229.2, score: 91, bangumiScore: 8.85,
  episodes: 28, season: 'FALL', year: 2026, status: 'RELEASING', format: 'TV', genres: ['Adventure', 'Drama', 'Fantasy', 'Comedy'],
  description: '勇者一行打倒魔王之后的故事。', ...over,
})

const airing = (anilistId: number, episode: number, airingAt: number, over: Partial<HomeAiringData> = {}): HomeAiringData => ({
  scheduleId: anilistId * 100 + episode, anilistId, episode, airingAt, title: '某部', hue: 10, ...over,
})

describe('评分的写法', () => {
  test('AniList 的 0–100 写成一位小数', () => {
    expect(scoreText(91)).toBe('9.1')
    expect(scoreText(80)).toBe('8.0')
  })

  test('没有评分是 null，不是「0.0」', () => {
    expect(scoreText(0)).toBeNull()
    expect(scoreText(undefined)).toBeNull()
  })

  test('Bangumi 的评分先按十分位取整（7.85 是 7.9，不是 7.8）', () => {
    expect(bangumiScoreText(7.85)).toBe('7.9')
    expect(bangumiScoreText(8)).toBe('8.0')
    expect(bangumiScoreText(0)).toBeNull()
  })
})

describe('episodesText', () => {
  test('「N 集」；不知道或 0 时为 null', () => {
    expect(episodesText(12)).toBe('12 集')
    expect(episodesText(null)).toBeNull()
    expect(episodesText(0)).toBeNull()
  })
})

describe('heroSlide', () => {
  test('没有横幅图时用封面顶替', () => {
    const s = heroSlide(row(), [])
    expect(s.banner).toBe('/art/c')
    expect(s.hasBanner).toBe(false)
  })

  test('有横幅图就用它', () => {
    const s = heroSlide(row({ banner: '/art/b' }), [])
    expect(s.banner).toBe('/art/b')
    expect(s.hasBanner).toBe(true)
  })

  test('最多三个类型，译成中文', () => {
    expect(heroSlide(row(), []).genres).toEqual(['冒险', '剧情', '奇幻'])
  })

  test('简介截到 160 个字', () => {
    const long = '字'.repeat(200)
    expect(heroSlide(row({ description: long }), []).synopsis).toBe(`${'字'.repeat(160)}...`)
  })

  test('放送表的秒换成毫秒、只取这一部、按先后排', () => {
    const s = heroSlide(row(), [airing(7, 9, 300), airing(8, 1, 100), airing(7, 8, 200)])
    expect(s.airings).toEqual([{ at: 200_000, ep: 8 }, { at: 300_000, ep: 9 }])
  })

  test('标题拆成逐块显现的小块；状态译成中文', () => {
    const s = heroSlide(row({ title: '葬送 FRIEREN' }), [])
    expect(s.tokens).toEqual(['葬', '送', ' ', 'FRIEREN'])
    expect(s.statusLabel).toBe('连载中')
  })
})

describe('seasonCard', () => {
  test('meta 是「类型 · 状态」', () => {
    expect(seasonCard(row(), null).meta).toBe('TV · 连载中')
  })

  test('剧场版的浮层不写集数', () => {
    expect(seasonCard(row({ format: 'MOVIE', episodes: 1 }), null).popMeta).toBe('剧场版 · 2026 秋季')
    expect(seasonCard(row(), null).popMeta).toBe('TV · 28 集 · 2026 秋季')
  })

  test('带着给它的下一集', () => {
    expect(seasonCard(row(), { at: 5, ep: 3 }).nextAiring).toEqual({ at: 5, ep: 3 })
  })
})

describe('gemCard', () => {
  test('优先用 Bangumi 评分并说明来源', () => {
    const g = gemCard(row())
    expect(g.score).toBe('8.9')
    expect(g.scoreSource).toBe('Bangumi')
  })

  test('Bangumi 没有时退到 AniList', () => {
    const g = gemCard(row({ bangumiScore: undefined }))
    expect(g.score).toBe('9.1')
    expect(g.scoreSource).toBe('AniList')
  })

  test('meta 是「季度 · 类型 · 集数」；原名与标题一样时不重复', () => {
    expect(gemCard(row()).meta).toBe('2026 秋季 · TV · 28 集')
    expect(gemCard(row({ titleNative: '葬送的芙莉莲' })).native).toBeNull()
  })
})

describe('trendCard / yearCard / hueCard / todayCard', () => {
  test('热门那一行先说 AniList，再说 Bangumi', () => {
    const ranked = (over: Partial<RankedMediaData>): RankedMediaData => ({ ...row(), rank: 1, watchers: 142, ...over })
    expect(trendCard(ranked({})).scoreLine).toBe('AniList 9.1')
    expect(trendCard(ranked({ score: undefined })).scoreLine).toBe('Bangumi 8.9')
    expect(trendCard(ranked({ score: undefined, bangumiScore: undefined })).scoreLine).toBe('')
  })

  test('年度榜只写季度（年份在标题里）', () => {
    expect(yearCard(row(), 3)).toMatchObject({ rank: 3, season: '秋季', score: '9.1' })
  })

  test('色调卡带着色相', () => {
    expect(hueCard(row()).hue).toBe(229.2)
    expect(hueCard(row({ hue: null })).hue).toBeNull()
  })

  test('今日更新的卡把秒换成毫秒；没有 scheduleId 时按作品与集号编一个', () => {
    expect(todayCard(airing(7, 3, 1000))).toMatchObject({ id: 7, key: 703, at: 1_000_000, ep: 3 })
    expect(todayCard(airing(7, 3, 1000, { scheduleId: 0 })).key).toBe(7003)
  })

  test('airingsOf 只取这一部', () => {
    expect(airingsOf([airing(1, 2, 10), airing(2, 1, 5)], 2)).toEqual([{ at: 5000, ep: 1 }])
  })
})
