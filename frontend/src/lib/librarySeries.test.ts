import { describe, expect, it } from 'vitest'
import type { LibraryCluster, LibraryItem } from './endpoints'
import { clusterWork, groupSeries, localEpisodeCount, mergeSeries, seriesProgress, versionLabel } from './librarySeries'

const item = (fileId: string, episode: number | null, extra: Partial<LibraryItem> = {}): LibraryItem => ({
  fileId, fileName: `${fileId}.mkv`, episode, kind: 'main', resolution: '1080p', sizeBytes: 1, progress: null, ...extra,
})
const cluster = (clusterKey: string, items: LibraryItem[], extra: Partial<LibraryCluster> = {}): LibraryCluster => ({
  clusterKey, title: clusterKey, season: null, confidence: 0.9, episodeCount: items.length,
  groups: [{ groupKey: clusterKey, label: clusterKey, sortMode: 'episode', items }], ...extra,
})

describe('clusterWork', () => {
  it('手动认定优先；标为不是目录作品的没有作品；否则用自动认出的', () => {
    const c = cluster('a', [], { matched: { anilistId: 2, title: '自动' } })
    expect(clusterWork(c)).toEqual({ anilistId: 2, title: '自动', confirmed: false })
    expect(clusterWork({ ...c, association: { mode: 'manual', anilistId: 1, title: '手动', setAt: 1 } })).toEqual({ anilistId: 1, title: '手动', confirmed: true })
    expect(clusterWork({ ...c, association: { mode: 'none', setAt: 1 } })).toBeUndefined()
    expect(clusterWork(cluster('b', []))).toBeUndefined()
  })
})

describe('groupSeries', () => {
  it('同一部作品的几个分组并成一部，位置跟第一次出现；没认出作品的各自成一部', () => {
    const series = groupSeries([
      cluster('ani', [item('a1', 1)], { matched: { anilistId: 9, title: 'Re：从零开始' } }),
      cluster('other', [item('o1', 1)]),
      cluster('lolihouse', [item('l1', 1), item('l2', 2)], { matched: { anilistId: 9, title: 'Re：从零开始' }, cover: '/art/x' }),
    ])
    expect(series.map(s => s.key)).toEqual(['a9', 'cother'])
    expect(series[0]!.title).toBe('Re：从零开始')
    // 正片多的版本排前面；封面取有封面的那个
    expect(series[0]!.clusters.map(c => c.clusterKey)).toEqual(['lolihouse', 'ani'])
    expect(series[0]!.cover).toBe('/art/x')
    expect(series[0]!.episodeCount).toBe(2)
  })

  it('手动认定过的版本排在最前，作品名用认定的', () => {
    const series = groupSeries([
      cluster('auto', [item('a1', 1), item('a2', 2)], { matched: { anilistId: 9, title: '自动认出的名字' } }),
      cluster('mine', [item('m1', 1)], { association: { mode: 'manual', anilistId: 9, title: '认定的名字', setAt: 1 } }),
    ])
    expect(series).toHaveLength(1)
    expect(series[0]!.clusters.map(c => c.clusterKey)).toEqual(['mine', 'auto'])
    expect(series[0]!.title).toBe('认定的名字')
    expect(series[0]!.work?.confirmed).toBe(true)
  })
})

describe('localEpisodeCount', () => {
  it('同一集的多个版本只算一次，没有集号的正片各算一集，附加内容不算', () => {
    const a = cluster('a', [item('a1', 1), item('a2', 2), item('op', null, { kind: 'ncop' })])
    const b = cluster('b', [item('b1', 1), item('b3', 3), item('sp', null)])
    expect(localEpisodeCount([a, b])).toBe(4)
    expect(localEpisodeCount([cluster('c', [item('x', null, { kind: 'ova' })])])).toBe(1)
  })
})

describe('mergeSeries', () => {
  it('只有一个分组时原样返回', () => {
    const a = cluster('a', [item('a1', 1)])
    const merged = mergeSeries([a])
    expect(merged.cluster).toBe(a)
    expect(merged.versions.size).toBe(0)
    expect(merged.owner.get('a1')).toBe(a)
  })

  it('每个集号一个主版本：正在看的 > 看完的 > 靠前的版本 > 分辨率高的；其余挂在它下面', () => {
    const a = cluster('a', [
      item('a1', 1),
      item('a2', 2),
      item('a3', 3, { resolution: '720p' }),
      item('op', null, { kind: 'ncop' }),
    ])
    const b = cluster('b', [
      item('b1', 1, { progress: { positionSec: 30, durationSec: 100, completed: false } }),
      item('b2', 2, { progress: { positionSec: 100, durationSec: 100, completed: true } }),
      item('b4', 4),
    ])
    const c = cluster('c', [item('c3', 3, { resolution: '2160p' })])
    const merged = mergeSeries([a, b, c])
    const [main, extras] = merged.cluster.groups
    expect(main!.items.map(i => i.fileId)).toEqual(['b1', 'b2', 'a3', 'b4'])
    expect(extras!.items.map(i => i.fileId)).toEqual(['op'])
    expect(merged.versions.get('b1')?.map(v => v.item.fileId)).toEqual(['a1'])
    expect(merged.versions.get('a3')?.map(v => v.item.fileId)).toEqual(['c3'])
    // 文件名里没有字幕组时用文件夹标题区分
    expect(merged.versions.get('a3')?.[0]?.label).toBe('c · 2160p')
    expect(merged.versions.has('b4')).toBe(false)
    expect(merged.owner.get('c3')).toBe(c)
    expect(merged.cluster.clusterKey).toBe('a')
    expect(merged.cluster.episodeCount).toBe(4)
  })

  it('两个文件夹里一模一样的副本只算一份', () => {
    const merged = mergeSeries([cluster('a', [item('same', 1)]), cluster('b', [item('same', 1)])])
    expect(merged.cluster.groups[0]!.items.map(i => i.fileId)).toEqual(['same'])
    expect(merged.versions.size).toBe(0)
  })
})

describe('mergeSeries：剧场版与没有集号的文件', () => {
  it('同一部剧场版的几个版本是一集，不是「剧集 0」加一堆特典', () => {
    const a = cluster('a', [item('a-movie', null, { kind: 'movie' })])
    const b = cluster('b', [item('b-movie', null, { kind: 'movie', group: 'XK' })])
    const merged = mergeSeries([a, b])
    expect(merged.cluster.groups.map(g => g.items.map(i => i.fileId))).toEqual([['a-movie']])
    expect(merged.versions.get('a-movie')?.map(v => v.label)).toEqual(['XK · 1080p'])
    expect(merged.cluster.episodeCount).toBe(1)
    expect(seriesProgress([a, { ...b, groups: [{ ...b.groups[0]!, items: [item('b-movie', null, { kind: 'movie', progress: { positionSec: 1, durationSec: 1, completed: true } })] }] }])).toEqual({ total: 1, completed: 1, started: 1 })
  })

  it('一个分组里好几个没有集号的正片：各算一集，不当成同一集的版本', () => {
    const a = cluster('a', [item('sp1', null), item('sp2', null)])
    const b = cluster('b', [item('b1', 1)])
    const merged = mergeSeries([a, b])
    expect(merged.cluster.groups[0]!.items.map(i => i.fileId)).toEqual(['b1', 'sp1', 'sp2'])
    expect(merged.versions.size).toBe(0)
    expect(localEpisodeCount([a, b])).toBe(3)
  })
})

describe('groupSeries：季数对不上', () => {
  it('认成同一部作品、季数却不一样：单列一张待确认的海报，不当成同一集的版本', () => {
    const s1 = cluster('s1', [item('a1', 1)], { season: 1, matched: { anilistId: 9, title: '作品' } })
    const s2 = cluster('s2', [item('b1', 1)], { season: 2, matched: { anilistId: 9, title: '作品' } })
    const noSeason = cluster('ns', [item('c1', 1)], { matched: { anilistId: 9, title: '作品' } })
    const series = groupSeries([s1, s2, noSeason])
    expect(series.map(s => s.key)).toEqual(['a9', 'a9~s2'])
    expect(series[0]!.clusters.map(c => c.clusterKey)).toEqual(['s1', 'ns'])
    expect(series[1]!.ambiguous).toBe(true)
  })

  it('标为「不是目录里的作品」的分组不参与按作品归组', () => {
    const none = cluster('n', [item('n1', 1)], { association: { mode: 'none', setAt: 1 }, matched: { anilistId: 9 } })
    const auto = cluster('m', [item('m1', 1)], { matched: { anilistId: 9, title: '作品' } })
    expect(groupSeries([none, auto]).map(s => s.key)).toEqual(['cn', 'a9'])
  })
})

describe('versionLabel', () => {
  it('字幕组 · 分辨率；文件名里没有字幕组时用文件夹标题；都没有时用文件名', () => {
    expect(versionLabel(item('x', 1, { group: 'ANi' }))).toBe('ANi · 1080p')
    expect(versionLabel(item('x', 1), cluster('LoliHouse 文件夹', []))).toBe('LoliHouse 文件夹 · 1080p')
    expect(versionLabel(item('x', 1, { resolution: null }))).toBe('x.mkv')
  })
})

describe('seriesProgress', () => {
  it('按集算：同一集的几个版本里看完任意一个就算看完这一集', () => {
    const done = { positionSec: 100, durationSec: 100, completed: true }
    const half = { positionSec: 30, durationSec: 100, completed: false }
    const a = cluster('a', [item('a1', 1, { progress: done }), item('a2', 2), item('op', null, { kind: 'ncop', progress: done })])
    const b = cluster('b', [item('b1', 1), item('b2', 2, { progress: half })])
    expect(seriesProgress([a, b])).toEqual({ total: 2, completed: 1, started: 2 })
    expect(seriesProgress([cluster('m', [item('movie', null, { kind: 'movie', progress: done })])])).toEqual({ total: 1, completed: 1, started: 1 })
  })
})
