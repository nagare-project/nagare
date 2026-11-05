import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { CheckContext, type Schema } from '../test/contract'
import * as c from '../test/apiContracts'
import { pluginSearchEvent, searchResult } from '../test/smokeContracts'

/**
 * 界面冒烟测试的假数据（e2e/fixtures，tools/uismoke 按 routes.json 回答）逐份严格解码，
 * 用的是与黄金文件同一套 schema（test/apiContracts.ts；还没有黄金文件的接口在 test/smokeContracts.ts）：
 * 假数据跟着接口类型变，冒烟测试不会在一份早就过时的数据上跑绿。说明见 docs/api-goldens.md。
 *
 * 不放进 apiGoldens.test.ts：smokeContracts 里的 schema 一导入就登记进覆盖检查（test/contract.ts），
 * 那边会因为它们没在黄金文件里出现而失败。vitest 每个测试文件各有一份模块，这里导入不影响那边。
 */

const FIXTURE_DIR = fileURLToPath(new URL('../../e2e/fixtures/', import.meta.url))
const EXPAND_CASES = fileURLToPath(new URL('../../../tools/uismoke/testdata/expand-now.json', import.meta.url))

type Contract = Pick<Schema<unknown>, 'check'>

/** routes.json 里每一条路由用哪个 schema 解码（数据文件只写信封里的 data 一段） */
const FIXTURE_CONTRACTS: Record<string, Contract> = {
  'GET /api/settings': c.settingsData,
  'GET /api/update': c.updateView,
  'GET /api/library': c.libraryData,
  'GET /api/player/status': c.playerStatus,
  'POST /api/play': c.playData,
  'GET /api/discover': c.discoverData,
  'GET /api/seasonal': c.seasonalData,
  'GET /api/schedule': c.scheduleData,
  'GET /api/lists': c.collectionData,
  'GET /api/catalog/search': c.catalogSearchData,
  'GET /api/anime/{id}': c.summaryMedia,
  'GET /api/anime/{id}/episodes': c.episodeMetadataList,
  'GET /api/anime/{id}/episode-offset': c.episodeOffset,
  'GET /api/sources': c.sourcesData,
  'GET /api/source-plugin': c.sourcePluginView,
  'GET /api/torrent/status': c.torrentStatus,
  'POST /api/torrent/play': c.torrentPlayData,
  'GET /api/downloads': c.downloadsData,
  'GET /api/search': searchResult,
  // 逐行事件流（.ndjson）：每一行各解一次
  'GET /api/search/plugin': pluginSearchEvent,
}

const NOW_TOKEN = /"@now(ms|iso)?([+-][0-9]+)?"/g

/**
 * 与 tools/uismoke 的 expandNow 同一套规则：时间占位串换成 now 加偏移（秒）后的 Unix 秒 / 毫秒 / RFC 3339 字符串。
 * 两边共用 tools/uismoke/testdata/expand-now.json 里的用例，Go 与这里各跑一遍，规则改了一边另一边就红。
 */
function expandNow(body: string, nowSec: number): string {
  return body.replace(NOW_TOKEN, (_match, kind: string | undefined, offset: string | undefined) => {
    const at = nowSec + (offset === undefined ? 0 : Number(offset))
    if (kind === 'ms') return String(at * 1000)
    if (kind === 'iso') return JSON.stringify(new Date(at * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z'))
    return String(at)
  })
}

/** 固定的「现在」：解码只看形状，不看具体时间 */
const NOW_SEC = 1_800_000_000

const routes = JSON.parse(readFileSync(join(FIXTURE_DIR, 'routes.json'), 'utf8')) as Record<string, string>

/** 读一份假数据、换掉时间占位串；.ndjson 拆成逐行的 JSON */
function readFixture(file: string): unknown[] {
  const body = expandNow(readFileSync(join(FIXTURE_DIR, file), 'utf8'), NOW_SEC)
  if (!file.endsWith('.ndjson')) return [JSON.parse(body)]
  return body.split('\n').filter(line => line.trim() !== '').map(line => JSON.parse(line) as unknown)
}

describe('冒烟测试的假数据', () => {
  it('routes.json 里每条路由都登记了解码契约，登记的也都还在路由表里', () => {
    expect(Object.keys(routes).filter(route => !(route in FIXTURE_CONTRACTS)).sort()).toEqual([])
    expect(Object.keys(FIXTURE_CONTRACTS).filter(route => !(route in routes)).sort()).toEqual([])
  })

  it.each(Object.entries(routes).filter(([route]) => route in FIXTURE_CONTRACTS))('%s（%s）能被严格解码', (route, file) => {
    const values = readFixture(file)
    expect(values.length).toBeGreaterThan(0)
    const ctx = new CheckContext()
    values.forEach((value, i) => FIXTURE_CONTRACTS[route]?.check(value, values.length > 1 ? `${file}:${i + 1}` : file, ctx))
    expect(ctx.errors).toEqual([])
  })

  it('换完时间占位串之后不剩 @now（写错的占位串 uismoke 也不认，会原样发给页面）', () => {
    const leftovers = Object.values(routes).filter(file => expandNow(readFileSync(join(FIXTURE_DIR, file), 'utf8'), NOW_SEC).includes('"@now'))
    expect(leftovers).toEqual([])
  })
})

describe('时间占位串的换法与 tools/uismoke 一致', () => {
  const { now, cases } = JSON.parse(readFileSync(EXPAND_CASES, 'utf8')) as { now: number; cases: { name: string; in: string; want: string }[] }
  it.each(cases)('$name', ({ in: input, want }) => {
    expect(expandNow(input, now)).toBe(want)
  })
})
