import { describe, expect, it } from 'vitest'
import type { LibraryCluster, LibraryData } from '../../lib/endpoints'
import { resolveLocalCluster } from './localAssociation'

const base: LibraryCluster = { clusterKey: 'a', title: 'A', season: null, confidence: 1, episodeCount: 1, groups: [] }
const lib = (...clusters: LibraryCluster[]): LibraryData => ({ clusters, folders: [], scannedAt: null, continueWatching: [] })
const manual = (key: string, anilistId: number): LibraryCluster => ({ ...base, clusterKey: key, association: { mode: 'manual', anilistId, setAt: 1 } })

describe('resolveLocalCluster', () => {
  it('后端认定的优先；作品已经认定了分组，本机旧记录就作废（免得那条认定撤掉后旧选择悄悄复活）', () => {
    const r = resolveLocalCluster(lib(base, manual('b', 9)), 9, 'a')
    expect(r.selected?.clusterKey).toBe('b')
    expect(r.via).toBe('manual')
    expect(r.dropLegacy).toBe(true)
    expect(resolveLocalCluster(lib(manual('b', 9)), 9, null).dropLegacy).toBe(false)
  })

  it('认定了不止一个分组：不代选，交给用户', () => {
    const r = resolveLocalCluster(lib(manual('a', 9), manual('b', 9)), 9, null)
    expect(r.selected).toBeUndefined()
    expect(r.associated.map(c => c.clusterKey)).toEqual(['a', 'b'])
  })

  it('本机旧记录：分组还在且没有后端决定时采用；有了别的决定就作废', () => {
    expect(resolveLocalCluster(lib(base), 9, 'a')).toMatchObject({ via: 'legacy', dropLegacy: false })
    const decided = resolveLocalCluster(lib(manual('a', 1)), 9, 'a')
    expect(decided.selected).toBeUndefined()
    expect(decided.dropLegacy).toBe(true)
    const none = resolveLocalCluster(lib({ ...base, association: { mode: 'none', setAt: 1 } }), 9, 'a')
    expect(none.dropLegacy).toBe(true)
  })

  it('旧记录指向的分组不在了：报告出来（盘可能没插，记录先留着）', () => {
    const r = resolveLocalCluster(lib(base), 9, 'gone')
    expect(r.legacyMissing).toBe(true)
    expect(r.dropLegacy).toBe(false)
    expect(r.selected).toBeUndefined()
  })

  it('自动匹配到这部作品的分组恰好一个时才代选', () => {
    const matched = { ...base, matched: { anilistId: 9 } }
    expect(resolveLocalCluster(lib(matched), 9, null)).toMatchObject({ via: 'matched' })
    expect(resolveLocalCluster(lib(matched, { ...matched, clusterKey: 'b' }), 9, null).selected).toBeUndefined()
    // 认定为别的作品的分组，即使自动匹配曾指向这部也不算
    expect(resolveLocalCluster(lib({ ...manual('a', 1), matched: { anilistId: 9 } }), 9, null).selected).toBeUndefined()
  })
})
