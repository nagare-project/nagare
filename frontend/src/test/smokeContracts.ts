/**
 * 还没有黄金文件的接口的严格解码 schema：目前只给界面冒烟测试的假数据（e2e/fixtures）用，见 lib/smokeFixtures.test.ts。
 *
 * 与 apiContracts.ts 一样，每个 schema 都钉在客户端类型上（exact），类型改了这里跟着改。
 * 区别是这里的 schema 不参与覆盖检查（withoutCoverage）：覆盖检查要求每个可选字段都在黄金文件里出现过，
 * 这些接口还没有黄金文件。给它们补上黄金文件之后，把 schema 挪进 apiContracts.ts。
 */
import type { PluginSearchEvent, SearchItem, SearchResult, SourceOutcome } from '../lib/endpoints'
import { arr, exact, int, lit, nullable, obj, str, union, withoutCoverage } from './contract'

/**
 * GET /api/search/plugin 一行事件在线上的形状。来源结果在线上也写作 event 为 "item"、带 outcome 字段
 * （Go 的 PluginSearchEvent），streamPluginMagnets 读的时候才改叫 'outcome'。
 */
type PluginSearchWireEvent = Exclude<PluginSearchEvent, { event: 'outcome' }> | { event: 'item'; outcome: SourceOutcome }

export const { searchResult, pluginSearchEvent } = withoutCoverage(() => {
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
  return {
    /** GET /api/search 的 data */
    searchResult: exact<SearchResult>()(obj({ query: str, items: arr(searchItem), sources: arr(sourceOutcome) })),
    pluginSearchEvent: exact<PluginSearchWireEvent>()(union(
      obj({ event: lit('scope'), scope: lit('all', 'episode') }),
      obj({ event: lit('item'), item: searchItem }),
      obj({ event: lit('item'), outcome: sourceOutcome }),
      obj({ event: lit('done') }),
    )),
  }
})
