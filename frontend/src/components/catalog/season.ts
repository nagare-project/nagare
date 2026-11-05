import type { SeasonName } from '../../lib/endpoints'

/** 季度顺序：冬（1–3 月）→ 春 → 夏 → 秋 */
export const SEASONS: readonly SeasonName[] = ['WINTER', 'SPRING', 'SUMMER', 'FALL']
export const SEASON_LABEL: Record<SeasonName, string> = { WINTER: '冬', SPRING: '春', SUMMER: '夏', FALL: '秋' }

/** 季度页的排序（默认按评分，不写进地址）。路由校验与页面共用这一份 */
export const SEASONAL_SORTS = ['score', 'title', 'format'] as const
export type SeasonalSortKey = (typeof SEASONAL_SORTS)[number]

/** 当前所处的季度（本地时间），用作默认值与年份下拉的上限。 */
export function currentSeason(now = new Date()): { season: SeasonName; year: number } {
  return { season: SEASONS[Math.floor(now.getMonth() / 3)]!, year: now.getFullYear() }
}

/** 相邻季度，跨年回绕：冬 <年> 的上一季是秋 <年-1>。 */
export function adjacentSeason(season: SeasonName, year: number, dir: -1 | 1): { season: SeasonName; year: number } {
  const next = SEASONS.indexOf(season) + dir
  if (next < 0) return { season: 'FALL', year: year - 1 }
  if (next >= SEASONS.length) return { season: 'WINTER', year: year + 1 }
  return { season: SEASONS[next]!, year }
}
