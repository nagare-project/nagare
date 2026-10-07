// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { clusterDisplayTitle, formatEpisodeList, needsConfirmation, setAssociation, dismissSyncedElsewhere } from './associations'
import type { LibraryCluster } from './endpoints'

function cluster(patch: Partial<LibraryCluster> = {}): LibraryCluster {
  return { clusterKey: 'k', title: '文件夹里的标题', season: null, confidence: 0.9, episodeCount: 2, groups: [], ...patch }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('formatEpisodeList', () => {
  it('连续的集并成区间，去重并排序', () => {
    expect(formatEpisodeList([5, 1, 2, 3, 3])).toBe('第 1–3、5 集')
    expect(formatEpisodeList([7])).toBe('第 7 集')
    expect(formatEpisodeList([1, 3, 5])).toBe('第 1、3、5 集')
    expect(formatEpisodeList([])).toBe('')
  })
})

describe('clusterDisplayTitle', () => {
  it('认定过作品就用作品名；标为不是目录作品或没认过时用文件夹标题', () => {
    expect(clusterDisplayTitle(cluster({ association: { mode: 'manual', anilistId: 1, title: '葬送的芙莉莲', setAt: 1 } }))).toBe('葬送的芙莉莲')
    expect(clusterDisplayTitle(cluster({ association: { mode: 'none', setAt: 1 } }))).toBe('文件夹里的标题')
    expect(clusterDisplayTitle(cluster())).toBe('文件夹里的标题')
  })
})

describe('needsConfirmation', () => {
  it('归组置信度低、又没认过作品时才要确认', () => {
    expect(needsConfirmation(cluster({ confidence: 0.5 }))).toBe(true)
    expect(needsConfirmation(cluster({ confidence: 0.9 }))).toBe(false)
    expect(needsConfirmation(cluster({ confidence: 0.5, association: { mode: 'none', setAt: 1 } }))).toBe(false)
  })
})

describe('请求形状', () => {
  function stubFetch(data: unknown) {
    const mock = vi.fn(async () => new Response(JSON.stringify({ success: true, data }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', mock)
    return mock
  }

  it('设定关联走 PUT，带 JSON 体；回到自动匹配返回 null', async () => {
    const mock = stubFetch({ association: null })

    await expect(setAssociation('a/b#S1', { mode: 'auto' })).resolves.toBeNull()

    const [path, init] = mock.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/library/association')
    expect(init.method).toBe('PUT')
    expect(JSON.parse(String(init.body))).toEqual({ clusterKey: 'a/b#S1', mode: 'auto' })
    expect(new Headers(init.headers).get('Content-Type')).toBe('application/json')
  })

  it('去掉一条「写到别的作品」记录走 POST', async () => {
    const association = { mode: 'manual', anilistId: 1, setAt: 1 }
    const mock = stubFetch({ association })

    await expect(dismissSyncedElsewhere('k', 999)).resolves.toEqual(association)

    const [path, init] = mock.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/library/association/dismiss-synced')
    expect(init.method).toBe('POST')
    expect(JSON.parse(String(init.body))).toEqual({ clusterKey: 'k', anilistId: 999 })
  })
})
