import { describe, expect, it } from 'vitest'
import type { LibraryCluster, LibraryGroup, LibraryItem } from '../../lib/endpoints'
import { displayGroups } from './displayGroups'

function item(fileName: string, episode: number | null): LibraryItem {
  return {
    fileId: `${fileName}|1|1`,
    fileName,
    episode,
    kind: 'main',
    resolution: '1080p',
    sizeBytes: 1,
    progress: null,
  }
}

function group(groupKey: string, label: string, items: LibraryItem[]): LibraryGroup {
  return { groupKey, label, sortMode: 'episode', items }
}

function cluster(groups: LibraryGroup[]): LibraryCluster {
  return { clusterKey: 'k', title: 'Fate strange Fake', season: null, confidence: 1, episodeCount: 3, groups }
}

describe('displayGroups', () => {
  it('根目录下逐个成组的散文件并回一个分区，按集号排序', () => {
    const ep2 = item('[LoliHouse] Fate strange Fake - 02.mkv', 2)
    const ep0 = item('[LoliHouse] Fate strange Fake - 00.mkv', 0)
    const ep1 = item('[LoliHouse] Fate strange Fake - 01.mkv', 1)
    const result = displayGroups(cluster([
      group(`__root__/${ep2.fileName}`, ep2.fileName, [ep2]),
      group(`__root__/${ep0.fileName}`, ep0.fileName, [ep0]),
      group(`__root__/${ep1.fileName}`, ep1.fileName, [ep1]),
    ]))

    expect(result).toHaveLength(1)
    expect(result[0]!.label).toBe('')
    expect(result[0]!.items.map((it) => it.episode)).toEqual([0, 1, 2])
  })

  it('根下只有一个文件时的 __root__ 哨兵也不当标题', () => {
    const only = item('movie.mkv', null)
    const result = displayGroups(cluster([group('__root__', '__root__', [only])]))

    expect(result).toEqual([group('__root__', '', [only])])
  })

  it('子目录分组原样保留，合并后的根分区留在第一个根分组的位置', () => {
    const season = group('Fate/Season 1', 'Season 1', [item('s1e1.mkv', 1)])
    const extras = group('Fate/SPs', 'SPs', [item('sp1.mkv', null)])
    const loose = item('loose - 05.mkv', 5)
    const result = displayGroups(cluster([season, group(`__root__/${loose.fileName}`, loose.fileName, [loose]), extras]))

    expect(result.map((g) => g.label)).toEqual(['Season 1', '', 'SPs'])
  })

  it('没有根分组时返回原数组', () => {
    const groups = [group('Fate/Season 1', 'Season 1', [item('s1e1.mkv', 1)])]
    const input = cluster(groups)

    expect(displayGroups(input)).toBe(groups)
  })
})
