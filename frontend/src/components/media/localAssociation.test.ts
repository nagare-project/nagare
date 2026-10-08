import { describe, expect, it } from 'vitest'
import type { LibraryCluster, LibraryData } from '../../lib/endpoints'
import { resolveLocalCluster } from './localAssociation'

const base: LibraryCluster = { clusterKey: 'a', title: 'A', season: null, confidence: 1, episodeCount: 1, groups: [] }
const lib = (...clusters: LibraryCluster[]): LibraryData => ({ clusters, folders: [], scannedAt: null, continueWatching: [] })
const manual = (key: string, anilistId: number): LibraryCluster => ({ ...base, clusterKey: key, association: { mode: 'manual', anilistId, setAt: 1 } })

describe('resolveLocalCluster', () => {
  it('后端认定的优先；作品已经认定了分组，本机旧记录就作废（免得那条认定撤掉后旧选择悄悄复活）', () => {
    const r = resolveLocalCluster(lib(base, manual('b', 9)), 9, 'a')
    expect(r.selected.map(c => c.clusterKey)).toEqual(['b'])
    expect(r.via).toBe('manual')
    expect(r.dropLegacy).toBe(true)
    expect(resolveLocalCluster(lib(manual('b', 9)), 9, null).dropLegacy).toBe(false)
  })

  it('认定了不止一个分组（同一部番的几个版本）：一起选中，剧集合并显示', () => {
    const r = resolveLocalCluster(lib(manual('a', 9), manual('b', 9)), 9, null)
    expect(r.selected.map(c => c.clusterKey)).toEqual(['a', 'b'])
    expect(r.via).toBe('manual')
    expect(r.associated.map(c => c.clusterKey)).toEqual(['a', 'b'])
  })

  it('认定过的版本在前，自动认出的版本跟着一起选中；含自动认出的就不算用户全确认过', () => {
    const r = resolveLocalCluster(lib({ ...base, clusterKey: 'm', matched: { anilistId: 9 } }, manual('a', 9)), 9, null)
    expect(r.selected.map(c => c.clusterKey)).toEqual(['a', 'm'])
    expect(r.via).toBe('matched')
  })

  it('本机旧记录：分组还在且没有后端决定时采用；有了别的决定就作废', () => {
    expect(resolveLocalCluster(lib(base), 9, 'a')).toMatchObject({ via: 'legacy', dropLegacy: false })
    const decided = resolveLocalCluster(lib(manual('a', 1)), 9, 'a')
    expect(decided.selected).toEqual([])
    expect(decided.dropLegacy).toBe(true)
    const none = resolveLocalCluster(lib({ ...base, association: { mode: 'none', setAt: 1 } }), 9, 'a')
    expect(none.dropLegacy).toBe(true)
  })

  it('旧记录指向的分组不在了：报告出来（盘可能没插，记录先留着）', () => {
    const r = resolveLocalCluster(lib(base), 9, 'gone')
    expect(r.legacyMissing).toBe(true)
    expect(r.dropLegacy).toBe(false)
    expect(r.selected).toEqual([])
  })

  it('自动认出是这部作品的分组都代选（几个版本合并显示），但不算用户确认过', () => {
    const matched = { ...base, matched: { anilistId: 9 } }
    expect(resolveLocalCluster(lib(matched), 9, null)).toMatchObject({ via: 'matched' })
    expect(resolveLocalCluster(lib(matched, { ...matched, clusterKey: 'b' }), 9, null).selected.map(c => c.clusterKey)).toEqual(['a', 'b'])
    // 季数和认定过的版本对不上的自动认出分组不代选（多半认错了季），仍可在列表里手动挑
    const s1 = { ...manual('s1', 9), season: 1 }
    const s2 = { ...base, clusterKey: 's2', season: 2, matched: { anilistId: 9 } }
    expect(resolveLocalCluster(lib(s1, s2), 9, null).selected.map(c => c.clusterKey)).toEqual(['s1'])
    // 认定为别的作品的分组，即使自动匹配曾指向这部也不算
    expect(resolveLocalCluster(lib({ ...manual('a', 1), matched: { anilistId: 9 } }), 9, null).selected).toEqual([])
  })
})
