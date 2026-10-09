// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installLocalStorage } from '../../test/storage'
import { mount } from '../../test/harness'
import { fetchSettings, fetchSources, searchMagnets, streamPluginMagnets } from '../../lib/endpoints'
import type { SearchItem } from '../../lib/endpoints'
import type { SettingsData, SourcesData } from '../../lib/endpoints'
import { fetchEpisodeOffset } from '../../lib/episodeOffset'
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
vi.mock('../../lib/episodeOffset', async original => ({
  ...await original<typeof import('../../lib/episodeOffset')>(),
  fetchEpisodeOffset: vi.fn(),
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
  vi.mocked(fetchEpisodeOffset).mockReset().mockResolvedValue({ known: true, offset: 0 })
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
const chip = (container: HTMLElement, episode: number) => container.querySelector<HTMLButtonElement>(`[aria-label="第 ${episode} 集"]`)!
const rows = (container: HTMLElement) => [...container.querySelectorAll<HTMLLIElement>('.media-resource-list li')]
const button = (container: HTMLElement, text: string) => [...container.querySelectorAll<HTMLButtonElement>('button')].find(item => item.textContent?.includes(text))

describe('作品页磁力：先选集数，再按字幕组挑这一集的发布', () => {
  it('默认选下一集没看的；插件按这一集搜索；播放带作品身份与条目自己的集号', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    expect(chip(container, 2).getAttribute('aria-pressed')).toBe('true')
    expect(container.querySelectorAll('.torrent-episode-chip')).toHaveLength(3)
    expect(container.querySelector('.torrent-episode-note')?.textContent).toContain('第二话的标题')
    expect(searchMagnets).not.toHaveBeenCalled()

    await search(container)
    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith(media.title, { altTitles: ['テスト'] })
    expect(vi.mocked(streamPluginMagnets).mock.calls[0]?.[1]).toEqual({ episode: 2, anilistId: 7, altTitles: ['テスト'] })
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0]).toEqual({ magnet, title: '[Group] 测试动画 02', episodeHint: 2, ...identity })
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
    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith('测试动画 1080p')
    await unmount()
  })

  it('没登录（看到第 0 集）也能换集：换集只重问插件，本机规则的结果沿用', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={{ ...media, watched: 0 }} inline />)
    expect(chip(container, 1).getAttribute('aria-pressed')).toBe('true')
    await search(container)
    await act(async () => chip(container, 3).click())
    expect(chip(container, 3).getAttribute('aria-pressed')).toBe('true')
    expect(searchMagnets).toHaveBeenCalledOnce()
    expect(vi.mocked(streamPluginMagnets).mock.calls.map(call => call[1].episode)).toEqual([1, 3])
    await unmount()
  })

  it('照 animego 只排序不隐藏：有这一集的字幕组在前，组内这一集的单集与合集在前，别的集、别的季、特典照常列出', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [
      release(1, 'A组', 2, { seeders: 3 }), release(2, 'A组', 1, { seeders: 50 }), batch(3, 'A组', 1, 12),
      batch(4, 'B组', 13, 24), release(5, 'B组', 2, { season: 2 }), release(6, 'C组', 2, { kind: 'sp' }),
      release(7, 'D组', undefined, { title: '[D组] 测试动画 合集', kind: 'batch' }),
    ] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    const groupNames = () => [...container.querySelectorAll('.media-fansub-chip strong')].map(node => node.textContent)
    expect(groupNames().slice(0, 2)).toEqual(['A组', 'D组'])
    expect(groupNames()).toEqual(expect.arrayContaining(['A组', 'B组', 'C组', 'D组']))
    expect(rows(container).map(row => row.querySelector('strong')?.textContent)).toEqual(['[A组] 测试动画 2', '[A组] 测试动画 [1-12]', '[A组] 测试动画 1'])
    expect(rows(container)[1]?.textContent).toContain('合集 第 1–12 集')
    expect(container.querySelectorAll('.media-resource-hit')).toHaveLength(2)
    expect(button(container, '显示全部发布')).toBeUndefined()
    expect(container.querySelector('.media-resource-results')?.textContent).toContain('7 个版本')
    await unmount()
  })

  it('这一集没有发布时说清楚，其余发布照常列出', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [release(1, 'A组', 1)] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    expect(container.querySelector('.media-resource-results')?.textContent).toContain('没有找到第 2 集的发布')
    expect(rows(container)).toHaveLength(1)
    await unmount()
  })

  it('插件按作品搜（scope=all）：换集只重新排序，不再问插件', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [] })
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => {
      onEvent({ event: 'scope', scope: 'all' })
      onEvent({ event: 'item', item: release(1, 'A组', 2, { source: 'plugin:garden' }) })
      onEvent({ event: 'item', item: release(2, 'A组', 3, { source: 'plugin:garden' }) })
      onEvent({ event: 'done' })
    })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    expect(rows(container)[0]?.querySelector('strong')?.textContent).toBe('[A组] 测试动画 2')
    await act(async () => chip(container, 3).click())
    expect(streamPluginMagnets).toHaveBeenCalledOnce()
    expect(rows(container).map(row => row.querySelector('strong')?.textContent)).toEqual(['[A组] 测试动画 3', '[A组] 测试动画 2'])
    await unmount()
  })

  it('播放合集用选中的集号定位文件（合集标题里的数字不是集号）', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [batch(3, 'A组', 1, 28)] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0]).toEqual({ magnet: batch(3, 'A组', 1, 28).magnet, title: '[A组] 测试动画 [1-28]', episodeHint: 2, ...identity })
    await unmount()
  })

  it('跨季连续编号：插件两种编号都问，标题写 14 的单集与 13-24 的合集都归到第 2 集', async () => {
    vi.mocked(fetchEpisodeOffset).mockResolvedValue({ known: true, offset: 12 })
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [] })
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => {
      onEvent({ event: 'item', item: release(8, 'ANi', 14, { source: 'plugin:garden', season: 2 }) })
      onEvent({ event: 'item', item: batch(9, 'B组', 13, 24) })
      onEvent({ event: 'done' })
    })
    const sequel = { ...media, title: '测试动画 第二季', episodes: 12 }
    const { container, unmount } = await mount(<MediaTorrentButton media={sequel} inline />)
    await search(container)
    expect(vi.mocked(streamPluginMagnets).mock.calls[0]?.[1]).toMatchObject({ episode: 2, absolute: 14 })
    expect(container.querySelector('.torrent-episode-note')?.textContent).toContain('第 14 集的发布也算这一集')
    expect(container.querySelector('.media-resource-list')?.textContent).toContain('第 14 集（连续编号，即第 2 集）')
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0]).toMatchObject({ episodeHint: 14 })
    expect(shared.play.mock.calls[0]?.[0].altEpisodeHint).toBeUndefined()

    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="字幕组 B组"]')!.click())
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[1]?.[0]).toMatchObject({ episodeHint: 14 })
    await unmount()
  })

  it('animego 算不出前作有几集（known=false）：不当 0 用，不问连续编号，并提示可能没归进来', async () => {
    vi.mocked(fetchEpisodeOffset).mockResolvedValue({ known: false, offset: 0 })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    expect(vi.mocked(streamPluginMagnets).mock.calls[0]?.[1].absolute).toBeUndefined()
    expect(container.querySelector('.torrent-episode-note')?.textContent).toContain('无法确认前作一共有几集')
    await unmount()
  })

  it('续作标题没写季数（用副标题）：标着 S2 的发布照样算这一集', async () => {
    vi.mocked(fetchEpisodeOffset).mockResolvedValue({ known: true, offset: 26 })
    vi.mocked(searchMagnets).mockResolvedValue({ query: '', sources: [], items: [
      release(1, 'A组', 2, { season: 2 }), release(2, 'B组', 28, { season: 2 }),
    ] })
    const { container, unmount } = await mount(<MediaTorrentButton media={{ ...media, title: '鬼灭之刃 游郭篇', titleNative: undefined }} inline />)
    await search(container)
    expect([...container.querySelectorAll('.media-fansub-chip strong')].map(node => node.textContent)).toEqual(['A组', 'B组'])
    await unmount()
  })

  it('OVA 作品自己的发布标着 OVA：也算这一集', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: '', sources: [], items: [release(1, 'A组', 2, { kind: 'ova' }), release(2, 'B组', 2, { kind: 'ncop' })] })
    const { container, unmount } = await mount(<MediaTorrentButton media={{ ...media, format: 'OVA' }} inline />)
    await search(container)
    expect(rows(container)).toHaveLength(1)
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0].episodeHint).toBe(2)
    await unmount()
  })

  it('还没搜过时点集数就直接按这一集搜', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await act(async () => chip(container, 3).click())
    expect(searchMagnets).toHaveBeenCalledOnce()
    expect(vi.mocked(streamPluginMagnets).mock.calls[0]?.[1].episode).toBe(3)
    await unmount()
  })

  it('查不到跨季编号时照常找源，并提示连续编号的发布可能没有归进来', async () => {
    vi.mocked(fetchEpisodeOffset).mockRejectedValue(new Error('502'))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    expect(vi.mocked(streamPluginMagnets).mock.calls[0]?.[1].absolute).toBeUndefined()
    expect(container.querySelector('.torrent-episode-note')?.textContent).toContain('暂时查不到跨季编号')
    expect(rows(container)).toHaveLength(1)
    await unmount()
  })

  it('插件指明合集里哪个文件就是这一集：播放直接带上文件下标', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [] })
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => {
      onEvent({ event: 'item', item: { ...batch(9, 'B组', 1, 12), source: 'plugin:garden', fileIndex: 4 } })
      onEvent({ event: 'done' })
    })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0]).toMatchObject({ suggestedFileIndex: 4, episodeHint: 2 })
    expect(shared.play.mock.calls[0]?.[0].fileIndex).toBeUndefined()
    await unmount()
  })

  it('插件来源中断要说出来，并能单独重试', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(streamPluginMagnets).mockRejectedValueOnce(new Error('插件掉线'))
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    const alert = [...container.querySelectorAll('[role="alert"]')].find(node => node.textContent?.includes('插件来源搜索中断'))
    expect(alert?.textContent).toContain('插件掉线')
    await act(async () => button(container, '重新搜索插件来源')!.click())
    expect(streamPluginMagnets).toHaveBeenCalledTimes(2)
    expect(searchMagnets).toHaveBeenCalledOnce()
    expect(container.textContent).not.toContain('插件来源搜索中断')
    await unmount()
  })

  it('剧场版不分集：不显示集数，也不按集筛', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [release(1, 'A组', undefined, { title: '[A组] 测试剧场版 [1080p]' })] })
    const movie = { ...media, format: 'MOVIE', episodes: 1, watched: 0 }
    const { container, unmount } = await mount(<MediaTorrentButton media={movie} inline />)
    expect(container.querySelector('.torrent-episode-picker')).toBeNull()
    await search(container)
    expect(rows(container)).toHaveLength(1)
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0]).toEqual({ magnet: release(1, 'A组').magnet, title: '[A组] 测试剧场版 [1080p]', ...identity })
    await unmount()
  })
})

describe('按字幕组浏览磁力版本', () => {
  it('同组合并、记住字幕组，合集优先；单集播放使用条目自己的集号', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [
      release(1, 'A组', 2, { seeders: 20 }), release(2, 'A组', 2, { seeders: 1, title: '[A组] 测试动画 02 v2' }),
      batch(3, 'A组', 1, 3), release(4, 'B组', 2),
    ] })
    localStorage.setItem('nagare:fansub:7', 'B组')
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    const chips = [...container.querySelectorAll<HTMLButtonElement>('.media-fansub-chip')]
    expect(chips.map(item => item.querySelector('strong')?.textContent)).toEqual(['B组', 'A组'])
    expect(chips[0]?.getAttribute('aria-pressed')).toBe('true')
    await act(async () => chips[1]!.click())
    expect(rows(container).map(row => row.querySelector('strong')?.textContent)).toEqual(['[A组] 测试动画 2', '[A组] 测试动画 02 v2', '[A组] 测试动画 [1-3]'])
    await act(async () => rows(container)[0]!.querySelector('button')!.click())
    expect(shared.play.mock.calls[0]?.[0].episodeHint).toBe(2)
    expect(localStorage.getItem('nagare:fansub:7')).toBe('A组')
    await unmount()
  })

  it('插件按作品搜还在流入时换集：不打断、不重问，后到的发布照样收进来', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [] })
    let emit: Parameters<typeof streamPluginMagnets>[2] | undefined
    let signal: AbortSignal | undefined
    let finish: (() => void) | undefined
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent, abort) => {
      emit = onEvent
      signal = abort
      await new Promise<void>(resolve => { finish = resolve })
    })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    await act(async () => { emit!({ event: 'scope', scope: 'all' }); emit!({ event: 'item', item: release(1, 'A组', 2) }) })
    await act(async () => chip(container, 3).click())
    await act(async () => emit!({ event: 'item', item: release(2, 'A组', 3) }))
    expect(streamPluginMagnets).toHaveBeenCalledOnce()
    expect(signal?.aborted).toBe(false)
    expect(rows(container).map(row => row.querySelector('strong')?.textContent)).toEqual(['[A组] 测试动画 3', '[A组] 测试动画 2'])
    await act(async () => { emit!({ event: 'done' }); finish!() })
    await unmount()
  })

  it('插件按作品搜完却一条都没有：换集时重问一次；老插件（scope=episode）每次换集都重问', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [] })
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, context, onEvent) => {
      onEvent({ event: 'scope', scope: context.episode === 2 ? 'all' : 'episode' })
      onEvent({ event: 'done' })
    })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    await act(async () => chip(container, 3).click())
    await act(async () => chip(container, 1).click())
    expect(vi.mocked(streamPluginMagnets).mock.calls.map(call => call[1].episode)).toEqual([2, 3, 1])
    await unmount()
  })

  it('有这一集的字幕组排在只有别的集的大组前面', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [
      release(1, '大组', 1), release(2, '大组', 3), release(3, '大组', 4), release(4, '小组', 2),
    ] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    expect([...container.querySelectorAll('.media-fansub-chip strong')].map(node => node.textContent)).toEqual(['小组', '大组'])
    await unmount()
  })

  it('跨来源去重的优先级：做种多的胜过高优先级来源；连着换两次也只剩一条', async () => {
    const base = release(1, 'A组', 2)
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [
      { ...base, source: 'plugin:garden', seeders: 3 }, { ...base, source: 'plugin:mikan', seeders: 9 },
      { ...release(2, 'B组', 2), source: 'plugin:nyaa' }, { ...release(2, 'B组', 2), source: 'plugin:garden' }, { ...release(2, 'B组', 2), source: 'plugin:tosho' },
    ] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="字幕组 A组"]')!.click())
    expect(rows(container)).toHaveLength(1)
    expect(rows(container)[0]?.textContent).toContain('插件 · mikan')
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="字幕组 B组"]')!.click())
    expect(rows(container)).toHaveLength(1)
    expect(rows(container)[0]?.textContent).toContain('插件 · tosho')
    await unmount()
  })

  it('同一种子跨来源去重：做种数相同时按 animego 的来源优先级保留（花园在 nyaa 之前）', async () => {
    const first = release(1, 'A组', 2, { source: 'plugin:nyaa' })
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [first, { ...first, source: 'plugin:garden' }, { ...first, source: 'plugin:mikan' }] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    expect(rows(container)).toHaveLength(1)
    expect(rows(container)[0]?.textContent).toContain('插件 · garden')
    await unmount()
  })

  it('同一种子跨来源去重，保留做种数，未识别字幕组仍可选择', async () => {
    const first = release(1, 'A组', 2)
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [first, { ...first, source: 'plugin:test', seeders: 42 }, release(2, '', 2)] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    const chips = [...container.querySelectorAll<HTMLButtonElement>('.media-fansub-chip')]
    expect(chips).toHaveLength(2)
    const a = chips.find(item => item.textContent?.includes('A组'))!
    await act(async () => a.click())
    expect(rows(container)).toHaveLength(1)
    expect(container.querySelector('.media-resource-list')?.textContent).toContain('做种 42')
    expect(chips.some(item => item.textContent?.includes('未分类'))).toBe(true)
    await unmount()
  })

  it('字幕组流式追加不会覆盖用户选择；旧作品的请求不能写入新作品', async () => {
    let emit: Parameters<typeof streamPluginMagnets>[2] | undefined
    let finish: (() => void) | undefined
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => { emit = onEvent; await new Promise<void>(resolve => { finish = resolve }) })
    const { container, rerender, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    await act(async () => emit!({ event: 'item', item: release(2, '新组', 2) }))
    const groupChip = container.querySelector<HTMLButtonElement>('[aria-label="字幕组 新组"]')!
    await act(async () => groupChip.click())
    await act(async () => emit!({ event: 'item', item: release(3, '另一组', 2) }))
    expect(groupChip.getAttribute('aria-pressed')).toBe('true')
    await rerender(<MediaTorrentButton media={{ ...media, id: 9, title: '另一部作品' }} inline />)
    await act(async () => { emit!({ event: 'item', item: release(4, '过期组', 2) }); finish!() })
    expect(container.querySelector('.media-fansub-browser')).toBeNull()
    await unmount()
  })

  it('偏移还没查回来时连点几集：只按最后点的那一集问插件；搜过的集不随观看进度漂移', async () => {
    let resolveOffset: ((value: { known: boolean; offset: number }) => void) | undefined
    vi.mocked(fetchEpisodeOffset).mockReturnValue(new Promise(resolve => { resolveOffset = resolve }))
    const { container, rerender, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    expect(streamPluginMagnets).not.toHaveBeenCalled()
    expect(container.textContent).toContain('插件来源仍在搜索')
    await act(async () => chip(container, 3).click())
    await act(async () => chip(container, 1).click())
    await act(async () => resolveOffset!({ known: true, offset: 0 }))
    expect(vi.mocked(streamPluginMagnets).mock.calls.map(call => call[1].episode)).toEqual([1])

    // 观看进度晚到（看到第 2 集）：用户选过的集不变
    await rerender(<MediaTorrentButton media={{ ...media, watched: 2 }} inline />)
    expect(chip(container, 1).getAttribute('aria-pressed')).toBe('true')
    await unmount()
  })

  it('没搜完就关窗口：再打开时可以重新搜索，插件来源说明只有一部分', async () => {
    let finishLocal: (() => void) | undefined
    vi.mocked(searchMagnets).mockReturnValueOnce(new Promise(resolve => { finishLocal = () => resolve({ query: media.title, items: [], sources: [] }) }))
    vi.mocked(streamPluginMagnets).mockImplementation(async () => { await new Promise<void>(() => {}) })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    const dialog = container.querySelector<HTMLDialogElement>('dialog')!
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    await search(container)
    await act(async () => dialog.close())
    await act(async () => finishLocal!())
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    const submit = container.querySelector<HTMLButtonElement>('.media-release-search button[type="submit"]')!
    expect(submit.disabled).toBe(false)
    expect(container.querySelector('.media-fansub-empty')).not.toBeNull()

    await search(container)
    await act(async () => dialog.close())
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-torrent')!.click())
    expect(container.textContent).toContain('窗口关闭时插件来源还没搜完')
    expect(button(container, '重新搜索插件来源')).toBeDefined()
    await unmount()
  })

  it('换集后上一集迟到的插件结果不能混进来', async () => {
    const emits: Parameters<typeof streamPluginMagnets>[2][] = []
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => { emits.push(onEvent); await new Promise<void>(() => {}) })
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    await act(async () => chip(container, 3).click())
    await act(async () => emits[0]!({ event: 'item', item: release(5, '旧集组', 3) }))
    expect(container.querySelector('[aria-label="字幕组 旧集组"]')).toBeNull()
    await act(async () => emits[1]!({ event: 'item', item: release(6, '新集组', 3) }))
    expect(container.querySelector('[aria-label="字幕组 新集组"]')).not.toBeNull()
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
