/**
 * 还没有黄金文件的接口的严格解码 schema：目前只给界面冒烟测试的假数据（e2e/fixtures）用，见 lib/smokeFixtures.test.ts。
 *
 * 为什么不放进 apiContracts.ts：那边的覆盖检查要求每个带可选字段的对象都在黄金文件里检查到过，
 * 没有黄金文件的 schema 放过去，那条检查就红了。给这些接口补上黄金文件之后，把 schema 挪过去。
 * 与 apiContracts.ts 一样，每个 schema 都钉在客户端类型上（exact），类型改了这里跟着改。
 */
import type { PluginSearchEvent, SearchItem, SearchResult, SourceOutcome } from '../lib/endpoints'
import { arr, exact, int, lit, nullable, obj, str, union } from './contract'

const searchItem = exact<SearchItem>()(obj(
  { title: str, magnet: str, size: str, fansub: nullable(str), date: nullable(str), source: str },
  {
    provider: str, seeders: int, infohash: str, episode: int, group: str, resolution: str, kind: str, torrentUrl: str,
    season: int, episodeRange: obj({ low: int, high: int }), fileIndex: int,
  },
))
const sourceOutcome = exact<SourceOutcome>()(obj(
  { source: str, state: lit('ok', 'zero', 'dead', 'failed', 'disabled'), count: int, rawCount: int, dropped: int, latencyMs: int },
  { fieldGaps: arr(str), reason: str, detail: str },
))

/** GET /api/search 的 data */
export const searchResult = exact<SearchResult>()(obj({ query: str, items: arr(searchItem), sources: arr(sourceOutcome) }))

type WireEvent<E extends PluginSearchEvent['event']> = Extract<PluginSearchEvent, { event: E }>

/**
 * GET /api/search/plugin 的一行事件（线上的形状）。来源结果在线上也写作 event 为 "item"、带 outcome 字段
 * （Go 的 PluginSearchEvent），streamPluginMagnets 读的时候才把它改叫 'outcome' —— 所以那一支不能直接钉在
 * PluginSearchEvent 上，只钉 outcome 的类型。
 */
export const pluginSearchEvent = union(
  exact<WireEvent<'scope'>>()(obj({ event: lit('scope'), scope: lit('all', 'episode') })),
  exact<WireEvent<'item'>>()(obj({ event: lit('item'), item: searchItem })),
  exact<{ event: 'item'; outcome: SourceOutcome }>()(obj({ event: lit('item'), outcome: sourceOutcome })),
  exact<WireEvent<'done'>>()(obj({ event: lit('done') })),
)
