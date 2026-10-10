// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installLocalStorage } from '../../test/storage'
import { mount } from '../../test/harness'
import { fetchSettings, fetchSources, searchMagnets, streamPluginMagnets } from '../../lib/endpoints'
import type { SearchItem } from '../../lib/endpoints'
import type { SettingsData, SourcesData } from '../../lib/endpoints'
import { MediaTorrentButton } from './MediaTorrentButton'
import { airedEpisodeCount } from './releaseEpisodes'

const shared = vi.hoisted(() => ({ play: vi.fn() }))
vi.mock('../torrent/TorrentPlayContext', () => ({
  useTorrentPlayback: () => ({ state: { phase: 'idle' }, status: null, zeroPeerSeconds: 0, busy: false, play: shared.play, selectFile: vi.fn(), retry: vi.fn(), cancel: vi.fn() }),
}))
vi.mock('../../lib/endpoints', async original => ({
  ...await original<typeof import('../../lib/endpoints')>(),
  fetchSettings: vi.fn(), fetchSources: vi.fn(), searchMagnets: vi.fn(), streamPluginMagnets: vi.fn(),
}))

const media = {
  id: 7, title: '测试动画', titleNative: 'テスト', episodes: 3, watched: 1, genres: [], status: 'FINISHED',
  episodeTitles: [{ episode: 2, title: '第二话的标题' }],
}
const identity = { anilistId: 7, titles: ['测试动画', 'テスト'] }
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

const release = (id: number, group: string, episode?: number, extra: Partial<SearchItem> = {}): SearchItem => ({
  title: `[${group}] 测试动画 ${episode ?? '合集'}`, magnet: `magnet:?xt=urn:btih:${String(id).padStart(40, '0')}`,
  size: '1 GB', fansub: null, date: null, source: 'local-rule', group, episode, kind: 'main', resolution: '1080p', ...extra,
})
const batch = (id: number, group: string, low: number, high: number): SearchItem =>
  release(id, group, undefined, { title: `[${group}] 测试动画 [${low}-${high}]`, kind: 'batch', episodeRange: { low, high } })

beforeEach(() => {
  shared.play.mockReset()
  vi.mocked(fetchSources).mockReset().mockResolvedValue(sources)
  installLocalStorage()
  vi.mocked(streamPluginMagnets).mockReset().mockImplementation(async (_q, _c, onEvent) => { onEvent({ event: 'done' }) })
  vi.mocked(fetchSettings).mockReset().mockResolvedValue(settings)
  vi.mocked(searchMagnets).mockReset().mockResolvedValue({
    query: media.title,
    items: [{ title: '[Group] 测试动画 02', magnet, size: '1 GB', fansub: '字幕组', date: null, source: 'local-rule', seeders: 8, episode: 2, kind: 'main' }],
    sources: [{ source: 'local-rule', state: 'ok', count: 1, rawCount: 1, dropped: 0, latencyMs: 10 }],
  })
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.open = true } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { if (!this.open) return; this.open = false; this.dispatchEvent(new Event('close')) } })
})

afterEach(() => {
  vi.restoreAllMocks()
  if (show) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', show); else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal')
  if (close) Object.defineProperty(HTMLDialogElement.prototype, 'close', close); else Reflect.deleteProperty(HTMLDialogElement.prototype, 'close')
})

const search = async (container: HTMLElement) => act(async () => {
  container.querySelector<HTMLFormElement>('.media-release-search')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
})
const rows = (container: HTMLElement) => [...container.querySelectorAll<HTMLLIElement>('.media-resource-list li')]
const titles = (container: HTMLElement) => rows(container).map(row => row.querySelector('strong')?.textContent)
const groupNames = (container: HTMLElement) => [...container.querySelectorAll('.media-fansub-chip strong')].map(node => node.textContent)
const button = (container: HTMLElement, text: string) => [...container.querySelectorAll<HTMLButtonElement>('button')].find(item => item.textContent?.includes(text))
const pick = async (container: HTMLElement, group: string) => act(async () => container.querySelector<HTMLButtonElement>(`[aria-label="字幕组 ${group}"]`)!.click())

describe('作品页磁力：不选集数，直接按字幕组分类', () => {
  it('切到磁力标签就直接搜，没有集数选择；单集播放带作品身份与条目自己的集号', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    expect(container.querySelector('.torrent-episode-picker')).toBeNull()
    expect(container.querySelector('[aria-label="集数"]')).toBeNull()
    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith(media.title, { altTitles: ['テスト'] })
    // 老插件只会按集号问：给它下一集没看的；按作品搜的插件不看这个
    expect(vi.mocked(streamPluginMagnets).mock.calls[0]?.[1]).toEqual({ episode: 2, anilistId: 7, altTitles: ['テスト'] })
    expect(container.querySelector('.media-resource-results')?.textContent).toContain('1 个字幕组 · 1 个版本')
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0]).toEqual({ magnet, title: '[Group] 测试动画 02', episodeHint: 2, ...identity })
    await unmount()
  })

  it('同一部作品重新渲染（观看进度晚到）不重复搜索；换了作品才重搜', async () => {
    const { rerender, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await rerender(<MediaTorrentButton media={{ ...media, watched: 2 }} inline />)
    expect(searchMagnets).toHaveBeenCalledOnce()
    await rerender(<MediaTorrentButton media={{ ...media, id: 9, title: '另一部作品', titleNative: undefined }} inline />)
    expect(vi.mocked(searchMagnets).mock.calls.map(call => call[0])).toEqual([media.title, '另一部作品'])
    await unmount()
  })

  it('改过搜索词就只搜用户写的，不再追加目录里的其他写法', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    const input = container.querySelector<HTMLInputElement>('.media-torrent-query input')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '测试动画 1080p')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await search(container)
    expect(vi.mocked(searchMagnets).mock.calls[1]).toEqual(['测试动画 1080p'])
    await unmount()
  })

  it('组内合集在前，正片按集号从小到大，特典与认不出集号的在后；什么都不隐藏', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [
      release(1, 'A组', 2, { seeders: 3 }), release(2, 'A组', 1, { seeders: 50 }), release(3, 'A组', 1, { kind: 'sp', title: '[A组] 测试动画 SP01' }),
      release(4, 'A组', undefined, { title: '[A组] 测试动画 PV' }), batch(5, 'A组', 1, 12),
      release(6, 'B组', 2), release(7, 'B组', 3, { season: 2 }),
    ] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    expect(groupNames(container)).toEqual(['A组', 'B组'])
    expect(titles(container)).toEqual(['[A组] 测试动画 [1-12]', '[A组] 测试动画 1', '[A组] 测试动画 2', '[A组] 测试动画 SP01', '[A组] 测试动画 PV'])
    expect(rows(container)[0]?.textContent).toContain('合集 第 1–12 集')
    expect(rows(container)[3]?.textContent).toContain('SP 1')
    expect(rows(container)[3]?.textContent).not.toContain('第 1 集')
    expect(button(container, '选择文件')).toBeDefined()
    expect(container.querySelector('.media-resource-results')?.textContent).toContain('2 个字幕组 · 7 个版本')
    await pick(container, 'B组')
    expect(titles(container)).toEqual(['[B组] 测试动画 2', '[B组] 测试动画 3'])
    await unmount()
  })

  it('同一集的几个版本：做种多的在前；字幕组按上次的选择排在最前并预选', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [
      release(1, 'A组', 2, { seeders: 1, title: '[A组] 测试动画 02 v2' }), release(2, 'A组', 2, { seeders: 20 }),
      batch(3, 'A组', 1, 3), release(4, 'B组', 2),
    ] })
    localStorage.setItem('nagare:fansub:7', 'B组')
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    const chips = [...container.querySelectorAll<HTMLButtonElement>('.media-fansub-chip')]
    expect(chips.map(item => item.querySelector('strong')?.textContent)).toEqual(['B组', 'A组'])
    expect(chips[0]?.getAttribute('aria-pressed')).toBe('true')
    await act(async () => chips[1]!.click())
    expect(titles(container)).toEqual(['[A组] 测试动画 [1-3]', '[A组] 测试动画 2', '[A组] 测试动画 02 v2'])
    await act(async () => rows(container)[1]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0].episodeHint).toBe(2)
    expect(localStorage.getItem('nagare:fansub:7')).toBe('A组')
    await unmount()
  })

  it('中文字幕优先：有中文字幕发布的字幕组排在更大的组前面；同一集的中文字幕版本在前', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [
      release(1, 'SubsPlease', 1), release(2, 'SubsPlease', 2), release(3, 'SubsPlease', 3),
      release(4, 'LoliHouse', 1, { seeders: 90, title: '[LoliHouse] 测试动画 01 [WebRip 1080p]' }),
      release(5, 'LoliHouse', 1, { seeders: 2, title: '[LoliHouse] 测试动画 01 [WebRip 1080p][简繁内封字幕]' }),
    ] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    expect(groupNames(container)).toEqual(['LoliHouse', 'SubsPlease'])
    expect(titles(container)).toEqual(['[LoliHouse] 测试动画 01 [WebRip 1080p][简繁内封字幕]', '[LoliHouse] 测试动画 01 [WebRip 1080p]'])
    await unmount()
  })

  it('标题写明了季数：这一季的发布排在前面，别的季照常列出', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: '', sources: [], items: [
      release(1, 'A组', 1, { season: 1, title: '[A组] 测试动画 S1 01' }), release(2, 'A组', 1, { season: 2, title: '[A组] 测试动画 S2 01' }),
    ] })
    const { container, unmount } = await mount(<MediaTorrentButton media={{ ...media, title: '测试动画 第二季' }} inline />)
    expect(titles(container)).toEqual(['[A组] 测试动画 S2 01', '[A组] 测试动画 S1 01'])
    await unmount()
  })

  it('播放合集不带集号：合集标题里的数字不是集号，交给播放前的文件列表', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [batch(3, 'A组', 1, 28)] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0]).toEqual({ magnet: batch(3, 'A组', 1, 28).magnet, title: '[A组] 测试动画 [1-28]', ...identity })
    await unmount()
  })

  it('插件指明了文件下标就照带', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [] })
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => {
      onEvent({ event: 'item', item: { ...batch(9, 'B组', 1, 12), source: 'plugin:garden', fileIndex: 4 } })
      onEvent({ event: 'done' })
    })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0]).toMatchObject({ suggestedFileIndex: 4 })
    expect(shared.play.mock.calls[0]?.[0].episodeHint).toBeUndefined()
    await unmount()
  })

  it('OVA 作品自己的发布标着 OVA：照常按集号定位', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: '', sources: [], items: [release(1, 'A组', 2, { kind: 'ova' })] })
    const { container, unmount } = await mount(<MediaTorrentButton media={{ ...media, format: 'OVA' }} inline />)
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0].episodeHint).toBe(2)
    await unmount()
  })

  it('老插件只会按集号搜（scope=episode）：说清楚插件来源只列出了哪一集', async () => {
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => {
      onEvent({ event: 'scope', scope: 'episode' })
      onEvent({ event: 'done' })
    })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    expect(container.textContent).toContain('插件来源只列出了第 2 集的发布')
    await unmount()
  })

  it('插件来源中断要说出来，并能单独重试', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(streamPluginMagnets).mockRejectedValueOnce(new Error('插件掉线'))
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    const alert = [...container.querySelectorAll('[role="alert"]')].find(node => node.textContent?.includes('插件来源搜索中断'))
    expect(alert?.textContent).toContain('插件掉线')
    await act(async () => button(container, '重新搜索插件来源')!.click())
    expect(streamPluginMagnets).toHaveBeenCalledTimes(2)
    expect(searchMagnets).toHaveBeenCalledOnce()
    expect(container.textContent).not.toContain('插件来源搜索中断')
    await unmount()
  })

  it('剧场版：同样按字幕组列出，播放不带集号', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [release(1, 'A组', undefined, { title: '[A组] 测试剧场版 [1080p]' })] })
    const movie = { ...media, format: 'MOVIE', episodes: 1, watched: 0 }
    const { container, unmount } = await mount(<MediaTorrentButton media={movie} inline />)
    expect(rows(container)).toHaveLength(1)
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0]).toEqual({ magnet: release(1, 'A组').magnet, title: '[A组] 测试剧场版 [1080p]', ...identity })
    await unmount()
  })
})

describe('按字幕组浏览磁力版本', () => {
  it('跨来源去重的优先级：做种多的胜过高优先级来源；连着换两次也只剩一条', async () => {
    const base = release(1, 'A组', 2)
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [
      { ...base, source: 'plugin:garden', seeders: 3 }, { ...base, source: 'plugin:mikan', seeders: 9 },
      { ...release(2, 'B组', 2), source: 'plugin:nyaa' }, { ...release(2, 'B组', 2), source: 'plugin:garden' }, { ...release(2, 'B组', 2), source: 'plugin:tosho' },
    ] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await pick(container, 'A组')
    expect(rows(container)).toHaveLength(1)
    expect(rows(container)[0]?.textContent).toContain('插件 · mikan')
    await pick(container, 'B组')
    expect(rows(container)).toHaveLength(1)
    expect(rows(container)[0]?.textContent).toContain('插件 · tosho')
    await unmount()
  })

  it('同一种子跨来源去重：做种数相同时按 animego 的来源优先级保留（花园在 nyaa 之前）', async () => {
    const first = release(1, 'A组', 2, { source: 'plugin:nyaa' })
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [first, { ...first, source: 'plugin:garden' }, { ...first, source: 'plugin:mikan' }] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    expect(rows(container)).toHaveLength(1)
    expect(rows(container)[0]?.textContent).toContain('插件 · garden')
    await unmount()
  })

  it('同一种子跨来源去重，保留做种数，未识别字幕组仍可选择', async () => {
    const first = release(1, 'A组', 2)
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [first, { ...first, source: 'plugin:test', seeders: 42 }, release(2, '', 2)] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    const chips = [...container.querySelectorAll<HTMLButtonElement>('.media-fansub-chip')]
    expect(chips).toHaveLength(2)
    await pick(container, 'A组')
    expect(rows(container)).toHaveLength(1)
    expect(container.querySelector('.media-resource-list')?.textContent).toContain('做种 42')
    expect(chips.some(item => item.textContent?.includes('未分类'))).toBe(true)
    await unmount()
  })

  it('字幕组流式追加不会覆盖用户选择；旧作品的请求不能写入新作品', async () => {
    const emits: Parameters<typeof streamPluginMagnets>[2][] = []
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => { emits.push(onEvent); await new Promise<void>(() => {}) })
    const { container, rerender, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await act(async () => emits[0]!({ event: 'item', item: release(2, '新组', 2) }))
    const groupChip = container.querySelector<HTMLButtonElement>('[aria-label="字幕组 新组"]')!
    await act(async () => groupChip.click())
    await act(async () => emits[0]!({ event: 'item', item: release(3, '另一组', 2) }))
    expect(groupChip.getAttribute('aria-pressed')).toBe('true')
    await rerender(<MediaTorrentButton media={{ ...media, id: 9, title: '另一部作品' }} inline />)
    await act(async () => emits[0]!({ event: 'item', item: release(4, '过期组', 2) }))
    expect(container.querySelector('[aria-label="字幕组 过期组"]')).toBeNull()
    await unmount()
  })

  it('重新搜索后上一次迟到的插件结果不能混进来', async () => {
    const emits: Parameters<typeof streamPluginMagnets>[2][] = []
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => { emits.push(onEvent); await new Promise<void>(() => {}) })
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    await act(async () => emits[0]!({ event: 'item', item: release(5, '旧组', 3) }))
    expect(container.querySelector('[aria-label="字幕组 旧组"]')).toBeNull()
    await act(async () => emits[1]!({ event: 'item', item: release(6, '新组', 3) }))
    expect(container.querySelector('[aria-label="字幕组 新组"]')).not.toBeNull()
    await unmount()
  })

  it('弹窗入口：打开才搜；已经搜过的再打开不重搜', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    expect(searchMagnets).not.toHaveBeenCalled()
    const dialog = container.querySelector<HTMLDialogElement>('dialog')!
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    expect(searchMagnets).toHaveBeenCalledOnce()
    expect(rows(container)).toHaveLength(1)
    await act(async () => dialog.close())
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    expect(searchMagnets).toHaveBeenCalledOnce()
    await unmount()
  })

  it('没搜完就关窗口：再打开时重新搜，插件来源说明只有一部分', async () => {
    let finishLocal: (() => void) | undefined
    vi.mocked(searchMagnets).mockReturnValueOnce(new Promise(resolve => { finishLocal = () => resolve({ query: media.title, items: [], sources: [] }) }))
    vi.mocked(streamPluginMagnets).mockImplementation(async () => { await new Promise<void>(() => {}) })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    const dialog = container.querySelector<HTMLDialogElement>('dialog')!
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    await act(async () => dialog.close())
    await act(async () => finishLocal!())
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    expect(searchMagnets).toHaveBeenCalledTimes(2)
    expect(rows(container)).toHaveLength(1)

    await act(async () => dialog.close())
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    expect(container.textContent).toContain('窗口关闭时插件来源还没搜完')
    expect(button(container, '重新搜索插件来源')).toBeDefined()
    await unmount()
  })

  it('搜索失败可重试，弹窗入口保留关闭与播放行为', async () => {
    vi.mocked(searchMagnets).mockRejectedValueOnce(new Error('暂时断线'))
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    expect(container.querySelector<HTMLDialogElement>('dialog')?.open).toBe(true)
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
