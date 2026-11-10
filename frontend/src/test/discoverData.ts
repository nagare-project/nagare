import type { DiscoverData, HomeMediaData } from '../lib/endpoints'

/** 单元测试用的发现页数据：默认每一块都是空的，按需覆盖 */
export function discoverData(overrides: Partial<DiscoverData> = {}): DiscoverData {
  return {
    season: 'FALL', year: 2026,
    seasonal: { items: [] }, seasonTotal: 0,
    trending: { items: [] }, gems: { items: [] }, yearly: { items: [] }, schedule: { items: [] },
    fetchedAt: 1,
    ...overrides,
  }
}

/** 一部作品的最少字段 */
export function homeMedia(overrides: Partial<HomeMediaData> & Pick<HomeMediaData, 'anilistId' | 'title'>): HomeMediaData {
  return { hue: null, episodes: null, genres: [], ...overrides }
}
