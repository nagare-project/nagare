// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installLocalStorage } from '../../test/storage'
import { mount } from '../../test/harness'
import { fetchSettings, fetchSources, searchMagnets } from '../../lib/endpoints'
import type { SettingsData, SourcesData } from '../../lib/endpoints'
import { airedEpisodeCount, MediaTorrentButton } from './MediaTorrentButton'

const shared = vi.hoisted(() => ({ play: vi.fn() }))
vi.mock('../torrent/TorrentPlayContext', () => ({
  useTorrentPlayback: () => ({ state: { phase: 'idle' }, status: null, zeroPeerSeconds: 0, busy: false, play: shared.play, selectFile: vi.fn(), retry: vi.fn(), cancel: vi.fn() }),
}))
vi.mock('../../lib/endpoints', async original => ({
  ...await original<typeof import('../../lib/endpoints')>(),
  fetchSettings: vi.fn(), fetchSources: vi.fn(), searchMagnets: vi.fn(),
}))

const media = {
  id: 7, title: '测试动画', episodes: 3, watched: 1, genres: [], status: 'FINISHED',
  episodeTitles: [{ episode: 2, title: '第二话的标题' }],
}
const sources: SourcesData = {
  sources: [{ id: 'local-rule', name: '本机规则', homepage: '', enabled: true, capabilities: { seeders: true, priority: 1 }, hasSelfTest: false }],
  rules: { remoteUrl: '', localDir: '/tmp/rules', dir: '/tmp/rules', loaded: 1, errors: [], lastLoadedAt: 1, lastSyncAt: null },
}
const settings = {
  torrent: { enabled: true },
} as SettingsData
const magnet = 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567'
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')

beforeEach(() => {
  shared.play.mockReset()
  vi.mocked(fetchSources).mockReset().mockResolvedValue(sources)
  installLocalStorage()
  vi.mocked(fetchSettings).mockReset().mockResolvedValue(settings)
  vi.mocked(searchMagnets).mockReset().mockResolvedValue({
    query: media.title,
    items: [{ title: '[Group] 测试动画 02', magnet, size: '1 GB', fansub: '字幕组', date: null, source: 'local-rule', seeders: 8 }],
    sources: [{ source: 'local-rule', state: 'ok', count: 1, rawCount: 1, dropped: 0, latencyMs: 10 }],
  })
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.open = true } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { if (!this.open) return; this.open = false; this.dispatchEvent(new Event('close')) } })
})

afterEach(() => {
  if (show) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', show); else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal')
  if (close) Object.defineProperty(HTMLDialogElement.prototype, 'close', close); else Reflect.deleteProperty(HTMLDialogElement.prototype, 'close')
})

describe('目录作品磁力选集', () => {
  it('点集数后用本机规则搜索，选择版本时把 episodeHint 交给共享播放会话', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    expect(container.querySelector<HTMLDialogElement>('.media-torrent-dialog')?.open).toBe(true)
    expect(container.querySelector('[aria-label="搜索第 2 集资源"]')?.textContent).toContain('第二话的标题')

    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="搜索第 2 集资源"]')!.click())
    await act(async () => {})
    // 带上集号与作品身份：后端据此再向来源插件要 BT 候选
    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith('测试动画', { episode: 2, anilistId: 7, altTitles: [] })
    expect(container.querySelector('.media-resource-list')?.textContent).toContain('[Group] 测试动画 02')
    expect(container.querySelector('.media-resource-list')?.textContent).toContain('本机规则')

    await act(async () => container.querySelector<HTMLButtonElement>('.media-resource-list button')!.click())
    expect(shared.play).toHaveBeenCalledWith(
      { magnet, title: '[Group] 测试动画 02', episodeHint: 2 },
      '[Group] 测试动画 02',
      expect.any(Function),
    )
    expect(container.querySelector<HTMLDialogElement>('.media-torrent-dialog')?.open).toBe(false)
    await unmount()
  })

  it('结果按字幕组分组：命中目标集的组在前、上次选过的预选、播放后记住字幕组', async () => {
    let n = 0
    // 每条一个不同的 infohash：选集按 infohash 折叠重复种子，同 hash 会被当成同一条
    const item = (title: string, group: string, episode: number | undefined, seeders?: number, kind = 'main') =>
      ({ title, magnet: `magnet:?xt=urn:btih:${String(++n).padStart(40, '0')}`, size: '1 GB', fansub: null, date: null, source: 'local-rule', group, episode, kind, resolution: '1080p', seeders })
    vi.mocked(searchMagnets).mockResolvedValue({
      query: '测试动画',
      sources: [{ source: 'local-rule', state: 'ok', count: 5, rawCount: 5, dropped: 0, latencyMs: 1 }],
      items: [
        item('[A] 测试动画 - 01', 'A组', 1),
        item('[B] 测试动画 - 02 弱种', 'B组', 2, 1),
        item('[B] 测试动画 - 02 强种', 'B组', 2, 20),
        item('[C] 测试动画 - 02', 'C组', 2, 5),
        item('[C] 测试动画 OP', 'C组', 2, 9, 'op'),
      ],
    })
    localStorage.setItem('nagare:fansub:7', 'C组')
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="搜索第 2 集资源"]')!.click())
    await act(async () => {})

    const chips = [...container.querySelectorAll<HTMLButtonElement>('.media-fansub-chip')]
    expect(chips.map(chip => chip.querySelector('strong')?.textContent)).toEqual(['C组', 'B组', 'A组'])
    expect(chips[0]?.getAttribute('aria-selected')).toBe('true')
    expect(chips[0]?.textContent).toContain('上次')
    expect(chips[2]?.textContent).toContain('无第 2 集')
    // C 组的 OP 不算命中：命中列表只有正片那一条，OP 折叠进「其他条目」
    expect(container.querySelectorAll('.media-fansub-detail > .media-resource-list li')).toHaveLength(1)
    expect(container.querySelector('.media-fansub-others summary')?.textContent).toContain('1')

    await act(async () => chips[1]!.click())
    const hits = [...container.querySelectorAll('.media-fansub-detail > .media-resource-list li')]
    expect(hits.map(li => li.querySelector('strong')?.textContent)).toEqual(['[B] 测试动画 - 02 强种', '[B] 测试动画 - 02 弱种'])
    expect(hits[0]?.querySelector('button')?.textContent).toBe('播放第 2 集')

    await act(async () => hits[0]!.querySelector('button')!.click())
    expect(shared.play).toHaveBeenCalledWith(expect.objectContaining({ episodeHint: 2, title: '[B] 测试动画 - 02 强种' }), expect.any(String), expect.any(Function))
    expect(localStorage.getItem('nagare:fansub:7')).toBe('B组')
    await unmount()
  })

  it('同一种子来自多个来源时按 infohash 折叠，保留带做种数的那条', async () => {
    const ih = '0123456789abcdef0123456789abcdef01234567'
    vi.mocked(searchMagnets).mockResolvedValue({
      query: '测试动画',
      sources: [{ source: 'garden', state: 'ok', count: 2, rawCount: 2, dropped: 0, latencyMs: 1 }, { source: 'plugin:nyaa', state: 'ok', count: 1, rawCount: 1, dropped: 0, latencyMs: 1 }],
      items: [
        { title: '[A] 测试动画 - 02', magnet: `magnet:?xt=urn:btih:${ih}&dn=x`, size: '1 GB', fansub: null, date: null, source: 'garden', group: 'A组', episode: 2, kind: 'main' },
        { title: '[A] 测试动画 - 02', magnet: `magnet:?xt=urn:btih:${ih.toUpperCase()}`, size: '1 GB', fansub: null, date: null, source: 'plugin:nyaa', group: 'A组', episode: 2, kind: 'main', seeders: 42, infohash: ih },
        { title: '[A] 测试动画 - 02 v2', magnet: 'magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98', size: '1 GB', fansub: null, date: null, source: 'garden', group: 'A组', episode: 2, kind: 'main' },
      ],
    })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="搜索第 2 集资源"]')!.click())
    await act(async () => {})
    const hits = [...container.querySelectorAll('.media-fansub-detail > .media-resource-list li')]
    expect(hits).toHaveLength(2)
    expect(hits[0]?.textContent).toContain('做种 42')
    await unmount()
  })

  it('没有任何组命中目标集时给出提示并展开该组全部条目', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({
      query: '测试动画',
      sources: [{ source: 'local-rule', state: 'ok', count: 1, rawCount: 1, dropped: 0, latencyMs: 1 }],
      items: [{ title: '测试动画 合集', magnet, size: '9 GB', fansub: null, date: null, source: 'local-rule' }],
    })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="搜索第 2 集资源"]')!.click())
    await act(async () => {})
    expect(container.textContent).toContain('没有识别到第 2 集的正片条目')
    expect(container.querySelector('.media-fansub-chip strong')?.textContent).toBe('未标注字幕组')
    expect(container.querySelector<HTMLDetailsElement>('.media-fansub-others')?.open).toBe(true)
    expect(container.querySelector('.media-fansub-others li span')?.textContent).toContain('未识别集数')
    await unmount()
  })

  it('未知总集数只列已有放送，未播作品不虚构集表', () => {
    expect(airedEpisodeCount({ ...media, episodes: null, status: 'RELEASING', nextAiring: { episode: 5, at: 1 } })).toBe(4)
    expect(airedEpisodeCount({ ...media, episodes: 12, watched: 0, status: 'NOT_YET_RELEASED' })).toBe(0)
    expect(airedEpisodeCount({ ...media, format: 'MOVIE', episodes: null })).toBe(1)
  })
})
