import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import type { DiscoverData, HomeMediaData } from '../lib/endpoints'

/** 单元测试用的发现页数据：默认每一块都是空的，按需覆盖 */
export function discoverFixture(overrides: Partial<DiscoverData> = {}): DiscoverData {
  return {
    season: 'FALL', year: 2026,
    seasonal: { items: [] }, seasonTotal: 0,
    trending: { items: [] }, gems: { items: [] }, yearly: { items: [] }, schedule: { items: [] },
    fetchedAt: 1,
    ...overrides,
  }
}

/** 一部作品的最少字段 */
export function homeMediaFixture(overrides: Partial<HomeMediaData> & Pick<HomeMediaData, 'anilistId' | 'title'>): HomeMediaData {
  return { hue: null, episodes: null, genres: [], ...overrides }
}

/** 与 tools/uismoke 同一个占位串规则（见 lib/smokeFixtures.test.ts） */
const NOW_TOKEN = /"@now(ms|iso)?([+-][0-9]{1,9})?"/g

/**
 * 界面冒烟测试那一份发现页假数据（e2e/fixtures/discover.json，严格解码过），时间占位串按 nowMs 展开。
 * 单元测试拿它当真实形状的数据用；vitest 的工作目录是 frontend/。
 */
export function smokeDiscover(nowMs: number = Date.now()): DiscoverData {
  const nowSec = Math.floor(nowMs / 1000)
  const raw = readFileSync(join(process.cwd(), 'e2e/fixtures/discover.json'), 'utf8')
  const body = raw.replace(NOW_TOKEN, (_match: string, kind: string | undefined, offset: string | undefined) => {
    const at = nowSec + (offset === undefined ? 0 : Number(offset))
    if (kind === 'ms') return String(at * 1000)
    if (kind === 'iso') return JSON.stringify(new Date(at * 1000).toISOString())
    return String(at)
  })
  return JSON.parse(body) as DiscoverData
}
