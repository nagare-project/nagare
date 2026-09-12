import { describe, expect, it } from 'vitest'
import { adjacentSeason, applySeasonalFilters, currentSeason } from './SeasonalPage'
import type { MediaSummary } from '../components/media/types'

const item = (id: number, over: Partial<MediaSummary>): MediaSummary => ({
  id, title: `作品${id}`, episodes: 12, watched: 0, genres: [], status: 'RELEASING', format: 'TV', ...over,
})

describe('季度浏览', () => {
  it('相邻季度跨年回绕：冬的上一季是去年秋，秋的下一季是明年冬', () => {
    expect(adjacentSeason('WINTER', 2026, -1)).toEqual({ season: 'FALL', year: 2025 })
    expect(adjacentSeason('FALL', 2026, 1)).toEqual({ season: 'WINTER', year: 2027 })
    expect(adjacentSeason('SPRING', 2026, 1)).toEqual({ season: 'SUMMER', year: 2026 })
  })

  it('当前季度按月份分桶', () => {
    expect(currentSeason(new Date(2026, 8, 12))).toEqual({ season: 'SUMMER', year: 2026 })
    expect(currentSeason(new Date(2026, 9, 1))).toEqual({ season: 'FALL', year: 2026 })
    expect(currentSeason(new Date(2026, 0, 1))).toEqual({ season: 'WINTER', year: 2026 })
  })

  it('筛选用 AniList 原始类型名，排序默认按评分、可按标题与格式', () => {
    const items = [
      item(1, { genres: ['Action'], score: 70, format: 'MOVIE' }),
      item(2, { genres: ['Romance'], score: 85, status: 'FINISHED' }),
      item(3, { genres: ['Action', 'Romance'], score: 90 }),
    ]
    expect(applySeasonalFilters(items, {}).map(i => i.id)).toEqual([3, 2, 1])
    expect(applySeasonalFilters(items, { genre: '动作' }).map(i => i.id)).toEqual([3, 1])
    expect(applySeasonalFilters(items, { format: 'MOVIE' }).map(i => i.id)).toEqual([1])
    expect(applySeasonalFilters(items, { status: 'FINISHED' }).map(i => i.id)).toEqual([2])
    expect(applySeasonalFilters(items, { sort: 'format' }).map(i => i.id)).toEqual([3, 2, 1])
    expect(applySeasonalFilters(items, { sort: 'title' }).map(i => i.title)).toEqual(['作品1', '作品2', '作品3'])
  })
})
