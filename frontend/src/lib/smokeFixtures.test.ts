import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { arr, CheckContext, int, obj, record, str, type Contract, type Schema } from '../test/contract'
import * as c from '../test/apiContracts'
import { pluginSearchEvent, searchResult } from '../test/smokeContracts'

/**
 * 界面冒烟测试的假数据（e2e/fixtures，tools/uismoke 按 routes.json 回答）逐份严格解码，
 * 用的是与黄金文件同一套 schema（test/apiContracts.ts；还没有黄金文件的接口在 test/smokeContracts.ts）：
 * 接口类型改了，假数据也得跟着改，冒烟测试不会在一份过时的数据上跑绿。说明见 docs/api-goldens.md。
 */

const FIXTURE_DIR = fileURLToPath(new URL('../../e2e/fixtures/', import.meta.url))
const EXPAND_CASES = fileURLToPath(new URL('../../../tools/uismoke/testdata/expand-now.json', import.meta.url))

/** routes.json 里每一条路由用哪个 schema 解码（数据文件只写信封里的 data 一段；.ndjson 逐行解） */
const FIXTURE_CONTRACTS: Record<string, Contract> = {
  'GET /api/settings': c.settingsData,
  'GET /api/update': c.updateView,
  'GET /api/library': c.libraryData,
  'GET /api/player/status': c.playerStatus,
  'POST /api/play': c.playData,
  'GET /api/discover': c.discoverData,
  'GET /api/discover/gems': c.gemsData,
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
  'GET /api/search/plugin': pluginSearchEvent,
}

/** 与 tools/uismoke 的 nowToken 同一个正则 */
const NOW_TOKEN = /"@now(ms|iso)?([+-][0-9]{1,9})?"/g

/**
 * 与 tools/uismoke 的 expandNow 同一套规则：时间占位串换成 now 加偏移（秒）后的 Unix 秒 / 毫秒 / RFC 3339 字符串。
 * 两边共用 tools/uismoke/testdata/expand-now.json 里的用例，Go 与这里各跑一遍，规则只改了一边就有一边红。
 * 这里的 now 是整秒（uismoke 用的是当时的时钟，毫秒那一档带零头），解码只看形状，不影响。
 */
function expandNow(body: string, nowSec: number): string {
  return body.replace(NOW_TOKEN, (_match: string, kind: string | undefined, offset: string | undefined) => {
    const at = nowSec + (offset === undefined ? 0 : Number(offset))
    if (kind === 'ms') return String(at * 1000)
    if (kind === 'iso') return JSON.stringify(new Date(at * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z'))
    return String(at)
  })
}

/** 读一份 JSON 并按 schema 校验；配置文件本身写坏了要报清楚是哪一份 */
function readChecked<T>(file: string, schema: Schema<T>): T {
  const value: unknown = JSON.parse(readFileSync(file, 'utf8'))
  const ctx = new CheckContext()
  schema.check(value, file, ctx)
  if (ctx.errors.length > 0) throw new Error(`${file} 的格式不对：${ctx.errors.join('；')}`)
  return value as T
}

/** 固定的「现在」：解码只看形状，不看具体时间 */
const NOW_SEC = 1_800_000_000

const routes = readChecked(join(FIXTURE_DIR, 'routes.json'), record(str))

function contractFor(route: string): Contract {
  if (!Object.hasOwn(FIXTURE_CONTRACTS, route)) throw new Error(`${route} 没有登记解码契约：在 FIXTURE_CONTRACTS 里写上它用哪个 schema`)
  return FIXTURE_CONTRACTS[route] as Contract
}

/** 读一份假数据、换掉时间占位串；.ndjson 拆成逐行的 JSON */
function readFixture(file: string): unknown[] {
  const body = expandNow(readFileSync(join(FIXTURE_DIR, file), 'utf8'), NOW_SEC)
  if (!file.endsWith('.ndjson')) return [JSON.parse(body)]
  return body.split('\n').filter(line => line.trim() !== '').map(line => JSON.parse(line) as unknown)
}

describe('冒烟测试的假数据', () => {
  it('routes.json 里每条路由都登记了解码契约，登记的也都还在路由表里', () => {
    const unmapped = Object.keys(routes).filter(route => !Object.hasOwn(FIXTURE_CONTRACTS, route)).sort()
    const stale = Object.keys(FIXTURE_CONTRACTS).filter(route => !Object.hasOwn(routes, route)).sort()
    expect(unmapped, '这些路由没有登记解码契约：在 FIXTURE_CONTRACTS 里写上它用哪个 schema').toEqual([])
    expect(stale, 'FIXTURE_CONTRACTS 里登记了、routes.json 里已经没有的路由：从表里删掉').toEqual([])
  })

  // 不按登记表过滤：没登记的路由在这里也会失败（contractFor 抛错），不会悄悄跳过
  it.each(Object.entries(routes))('%s（%s）能被严格解码', (route, file) => {
    const contract = contractFor(route)
    const values = readFixture(file)
    expect(values.length, `${file} 是空的`).toBeGreaterThan(0)
    const ctx = new CheckContext()
    values.forEach((value, i) => contract.check(value, values.length > 1 ? `${file}:${i + 1}` : file, ctx))
    expect(ctx.errors, `${file} 与接口类型对不上：改假数据，接口类型本身错了就同步改 TS 类型与 schema`).toEqual([])
  })

  it('换完时间占位串之后不剩以 @now 开头的字符串（写错的占位串 uismoke 也不认，会原样发给页面）', () => {
    const leftovers = Object.values(routes).flatMap(file =>
      (expandNow(readFileSync(join(FIXTURE_DIR, file), 'utf8'), NOW_SEC).match(/"@now[^"]*"/g) ?? []).map(token => `${file}: ${token}`),
    )
    expect(leftovers, '占位串只认 "@now" / "@nowms" / "@nowiso"，后面可跟 +N / -N 秒（N 最多 9 位）').toEqual([])
  })
})

describe('时间占位串的换法与 tools/uismoke 一致', () => {
  const corpus = readChecked(EXPAND_CASES, obj({ $comment: str, now: int, cases: arr(obj({ name: str, in: str, want: str })) }))

  it('共用的用例文件不是空的', () => {
    expect(corpus.cases.length).toBeGreaterThan(0)
  })

  it.each(corpus.cases)('$name', ({ in: input, want }) => {
    expect(expandNow(input, corpus.now)).toBe(want)
  })
})
