// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installLocalStorage } from '../../test/storage'
import { mount } from '../../test/harness'
import { fetchSettings, fetchSources, searchMagnets, streamPluginMagnets } from '../../lib/endpoints'
import type { SearchItem } from '../../lib/endpoints'
import type { SettingsData, SourcesData } from '../../lib/endpoints'
import { airedEpisodeCount, MediaTorrentButton } from './MediaTorrentButton'

const shared = vi.hoisted(() => ({ play: vi.fn() }))
vi.mock('../torrent/TorrentPlayContext', () => ({
  useTorrentPlayback: () => ({ state: { phase: 'idle' }, status: null, zeroPeerSeconds: 0, busy: false, play: shared.play, selectFile: vi.fn(), retry: vi.fn(), cancel: vi.fn() }),
}))
vi.mock('../../lib/endpoints', async original => ({
  ...await original<typeof import('../../lib/endpoints')>(),
  fetchSettings: vi.fn(), fetchSources: vi.fn(), searchMagnets: vi.fn(), streamPluginMagnets: vi.fn(),
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
  vi.mocked(streamPluginMagnets).mockReset().mockImplementation(async (_q, _c, onEvent) => { onEvent({ event: 'done' }) })
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


const search = async (container: HTMLElement) => act(async () => {
  container.querySelector<HTMLFormElement>('.media-release-search')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
})
const release = (id: number, group: string, episode?: number, extra: Partial<SearchItem> = {}): SearchItem => ({
  title: `[${group}] 测试动画 ${episode ?? '合集'}`, magnet: `magnet:?xt=urn:btih:${String(id).padStart(40, '0')}`,
  size: '1 GB', fansub: null, date: null, source: 'local-rule', group, episode, kind: 'main', resolution: '1080p', ...extra,
})

describe('按字幕组浏览磁力版本', () => {
  it('移除集数网格，先搜索字幕组；未知集号的合集不继承观看进度', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    expect(container.querySelector('.media-episode-grid')).toBeNull()
    expect(container.querySelector('input[type="number"]')).toBeNull()
    expect(searchMagnets).not.toHaveBeenCalled()
    await search(container)
    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith(media.title)
    expect(streamPluginMagnets).toHaveBeenCalledOnce()
    await act(async () => container.querySelector<HTMLButtonElement>('.media-resource-list button')!.click())
    expect(shared.play.mock.calls[0]?.[0]).toEqual({ magnet, title: '[Group] 测试动画 02' })
    await unmount()
  })

  it('同组所有集数合并，记住字幕组，合集优先，单集播放使用条目自己的集号', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [
      release(1, 'A组', 8, { seeders: 20 }), release(2, 'A组', 1, { seeders: 1 }),
      release(3, 'A组', undefined), release(4, 'B组', 2),
    ] })
    localStorage.setItem('nagare:fansub:7', 'B组')
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    const chips = [...container.querySelectorAll<HTMLButtonElement>('.media-fansub-chip')]
    expect(chips.map(chip => chip.querySelector('strong')?.textContent)).toEqual(['B组', 'A组'])
    expect(chips[0]?.getAttribute('aria-pressed')).toBe('true')
    await act(async () => chips[1]!.click())
    const rows = [...container.querySelectorAll('.media-resource-list li')]
    expect(rows).toHaveLength(3)
    expect(rows[0]?.textContent).toContain('合集')
    expect(rows[1]?.textContent).toContain('第 8 集')
    await act(async () => rows[1]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0].episodeHint).toBe(8)
    expect(localStorage.getItem('nagare:fansub:7')).toBe('A组')
    await unmount()
  })

  it('同一种子跨来源去重，保留做种数，未识别字幕组仍可选择', async () => {
    const first = release(1, 'A组', 3)
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [first, { ...first, source: 'plugin:test', seeders: 42 }, release(2, '', undefined)] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    const chips = [...container.querySelectorAll<HTMLButtonElement>('.media-fansub-chip')]
    expect(chips).toHaveLength(2)
    const a = chips.find(chip => chip.textContent?.includes('A组'))!
    await act(async () => a.click())
    expect(container.querySelectorAll('.media-resource-list li')).toHaveLength(1)
    expect(container.querySelector('.media-resource-list')?.textContent).toContain('做种 42')
    expect(chips.some(chip => chip.textContent?.includes('未分类'))).toBe(true)
    await unmount()
  })

  it('字幕组流式追加不会覆盖用户选择；旧作品的请求不能写入新作品', async () => {
    let emit: Parameters<typeof streamPluginMagnets>[2] | undefined
    let finish: (() => void) | undefined
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => { emit = onEvent; await new Promise<void>(resolve => { finish = resolve }) })
    const { container, rerender, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    await act(async () => emit!({ event: 'item', item: release(2, '新组', 5) }))
    const chip = container.querySelector<HTMLButtonElement>('[aria-label="字幕组 新组"]')!
    await act(async () => chip.click())
    await act(async () => emit!({ event: 'item', item: release(3, '另一组', 6) }))
    expect(chip.getAttribute('aria-pressed')).toBe('true')
    await rerender(<MediaTorrentButton media={{ ...media, id: 9, title: '另一部作品' }} inline />)
    await act(async () => { emit!({ event: 'item', item: release(4, '过期组', 1) }); finish!() })
    expect(container.querySelector('.media-fansub-browser')).toBeNull()
    await unmount()
  })

  it('搜索失败可重试，弹窗入口保留关闭与播放行为', async () => {
    vi.mocked(searchMagnets).mockRejectedValueOnce(new Error('暂时断线'))
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    expect(container.querySelector<HTMLDialogElement>('dialog')?.open).toBe(true)
    await search(container)
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('暂时断线')
    await act(async () => container.querySelector<HTMLButtonElement>('[role="alert"] button')!.click())
    expect(container.querySelector('.media-resource-list')).not.toBeNull()
    await act(async () => container.querySelector<HTMLButtonElement>('.media-resource-list button')!.click())
    expect(container.querySelector<HTMLDialogElement>('dialog')?.open).toBe(false)
    await unmount()
  })

  it('在线选集仍只列已播出的集数', () => {
    expect(airedEpisodeCount({ ...media, episodes: null, status: 'RELEASING', nextAiring: { episode: 5, at: 1 } })).toBe(4)
    expect(airedEpisodeCount({ ...media, episodes: 12, watched: 0, status: 'NOT_YET_RELEASED' })).toBe(0)
  })
})
