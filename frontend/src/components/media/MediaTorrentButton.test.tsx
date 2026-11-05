// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installLocalStorage } from '../../test/storage'
import { mount } from '../../test/harness'
import { accessibleName, getAllByRole, getByRole, queryByRole } from '../../test/queries'
import { addDownload, fetchSettings, fetchSources, searchMagnets, streamPluginMagnets } from '../../lib/endpoints'
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
  fetchSettings: vi.fn(), fetchSources: vi.fn(), searchMagnets: vi.fn(), streamPluginMagnets: vi.fn(), addDownload: vi.fn(),
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
  vi.mocked(addDownload).mockReset().mockImplementation(async (_locator, title) => ({ id: 'abc', title, root: '/dl', state: 'metadata', addedAt: 1, bytesDone: 0, peers: 0, seeders: 0, downRate: 0 }))
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

// 弹窗 / 抽屉没打开时读屏碰不到里面的东西；这里查的是面板的内容，一律带 hidden
const ALL = { hidden: true } as const
/** 作品名称输入框所在的表单：提交 = 搜索 */
const search = async (container: HTMLElement) => act(async () => {
  (getByRole(container, 'textbox', { name: '作品名称', ...ALL }) as HTMLInputElement).form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
})
const queryInput = (container: HTMLElement) => getByRole(container, 'textbox', { name: '作品名称', ...ALL }) as HTMLInputElement
/** 整块结果（按字幕组浏览） */
const results = (container: HTMLElement) => getByRole(container, 'region', { name: '按字幕组浏览磁力资源', ...ALL })
/** 当前选中的字幕组的发布列表（「<组名> 的资源」） */
const resources = (container: HTMLElement) => queryByRole(container, 'group', { name: /的资源$/, ...ALL })
const rows = (container: HTMLElement) => { const panel = resources(container); return panel === null ? [] : getAllByRole(panel, 'listitem', ALL) }
const titles = (container: HTMLElement) => rows(container).map(row => row.querySelector('strong')?.textContent)
/** 字幕组按钮：名字就是可见文字「字幕组 <组名> …」 */
const chips = (container: HTMLElement) => { const row = queryByRole(container, 'group', { name: '选择字幕组', ...ALL }); return row === null ? [] : getAllByRole(row, 'button', ALL) as HTMLButtonElement[] }
const groupNames = (container: HTMLElement) => chips(container).map(item => item.querySelector('strong')?.textContent?.replace(/^字幕组 /, ''))
const button = (container: HTMLElement, text: string) => getAllByRole(container, 'button', ALL).find(item => item.textContent?.includes(text)) as HTMLButtonElement | undefined
const chip = (container: HTMLElement, group: string) => chips(container).find(item => item.querySelector('strong')?.textContent === `字幕组 ${group}`) ?? null
/** 一行发布的主操作：单集「播放」，合集与认不出集号的「选择文件」 */
const playIn = (row: HTMLElement) => getByRole(row, 'button', { name: /^(播放|选择文件)$/, ...ALL }) as HTMLButtonElement
/** 卡片上的弹窗入口 */
const cardEntry = (container: HTMLElement) => getByRole(container, 'button', { name: `按字幕组查找磁力：${media.title}` })
const dialogOf = (container: HTMLElement) => getByRole(container, 'dialog', ALL) as HTMLDialogElement
const pick = async (container: HTMLElement, group: string) => act(async () => chip(container, group)!.click())

describe('作品页磁力：不选集数，直接按字幕组分类', () => {
  it('切到磁力标签就直接搜，没有集数选择；单集播放带作品身份与条目自己的集号', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    // 没有选集：没有「集数」一组按钮，也没有下拉
    expect(queryByRole(container, 'group', { name: '集数', ...ALL })).toBeNull()
    expect(queryByRole(container, 'combobox', ALL)).toBeNull()
    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith(media.title, { altTitles: ['テスト'] })
    // 老插件只会按集号问：给它下一集没看的；按作品搜的插件不看这个
    expect(vi.mocked(streamPluginMagnets).mock.calls[0]?.[1]).toEqual({ episode: 2, anilistId: 7, altTitles: ['テスト'] })
    expect(results(container).textContent).toContain('1 个字幕组 · 1 个版本')
    await act(async () => playIn(rows(container)[0]!).click())
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
    const input = queryInput(container)
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
    expect(results(container).textContent).toContain('2 个字幕组 · 7 个版本')
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
    const groups = chips(container)
    expect(groups.map(item => item.querySelector('strong')?.textContent)).toEqual(['字幕组 B组', '字幕组 A组'])
    expect(groups[0]?.getAttribute('aria-pressed')).toBe('true')
    await act(async () => groups[1]!.click())
    expect(titles(container)).toEqual(['[A组] 测试动画 [1-3]', '[A组] 测试动画 2', '[A组] 测试动画 02 v2'])
    await act(async () => playIn(rows(container)[1]!).click())
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
    await act(async () => playIn(rows(container)[0]!).click())
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
    await act(async () => playIn(rows(container)[0]!).click())
    expect(shared.play.mock.calls[0]?.[0]).toMatchObject({ suggestedFileIndex: 4 })
    expect(shared.play.mock.calls[0]?.[0].episodeHint).toBeUndefined()
    await unmount()
  })

  it('OVA 作品自己的发布标着 OVA：照常按集号定位', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: '', sources: [], items: [release(1, 'A组', 2, { kind: 'ova' })] })
    const { container, unmount } = await mount(<MediaTorrentButton media={{ ...media, format: 'OVA' }} inline />)
    await act(async () => playIn(rows(container)[0]!).click())
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
    const alert = getAllByRole(container, 'alert', ALL).find(node => node.textContent?.includes('插件来源搜索中断'))
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
    await act(async () => playIn(rows(container)[0]!).click())
    expect(shared.play.mock.calls[0]?.[0]).toEqual({ magnet: release(1, 'A组').magnet, title: '[A组] 测试剧场版 [1080p]', ...identity })
    await unmount()
  })
})

describe('磁力栏「下载」：完整下到下载目录，下完进媒体库', () => {
  it('点下载交给下载器（有种子文件地址优先用它），按钮变成已加入，并提示去哪看进度', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [
      batch(1, 'A组', 1, 12), release(2, 'A组', 1, { torrentUrl: 'https://example.invalid/a.torrent' }),
    ] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    const downloadOf = (index: number) => getAllByRole(rows(container)[index]!, 'button', ALL).find(item => item.textContent === '下载') as HTMLButtonElement | undefined
    await act(async () => downloadOf(0)!.click())
    expect(addDownload).toHaveBeenCalledExactlyOnceWith({ magnet: batch(1, 'A组', 1, 12).magnet }, '[A组] 测试动画 [1-12]')
    expect(rows(container)[0]?.textContent).toContain('已加入下载')
    expect(container.textContent).toContain('已加入下载：[A组] 测试动画 [1-12]。下完会自动出现在媒体库里。')
    expect(getByRole(container, 'link', { name: '查看下载', ...ALL }).getAttribute('href')).toBe('/torrents')
    expect(shared.play).not.toHaveBeenCalled()

    await act(async () => downloadOf(1)!.click())
    expect(vi.mocked(addDownload).mock.calls[1]?.[0]).toEqual({ torrentUrl: 'https://example.invalid/a.torrent' })
    await unmount()
  })

  it('加入失败：说出原因，按钮恢复可点', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(addDownload).mockRejectedValueOnce(new Error('这是私有站（PT）的种子，nagare 不下载'))
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    const download = () => getAllByRole(rows(container)[0]!, 'button', ALL).find(item => item.textContent === '下载') as HTMLButtonElement | undefined
    await act(async () => download()!.click())
    expect(container.textContent).toContain('私有站')
    expect(download()?.disabled).toBe(false)
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
    const groups = chips(container)
    expect(groups).toHaveLength(2)
    await pick(container, 'A组')
    expect(rows(container)).toHaveLength(1)
    expect(resources(container)?.textContent).toContain('做种 42')
    expect(groups.some(item => item.textContent?.includes('未分类'))).toBe(true)
    await unmount()
  })

  it('字幕组流式追加不会覆盖用户选择；旧作品的请求不能写入新作品', async () => {
    const emits: Parameters<typeof streamPluginMagnets>[2][] = []
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => { emits.push(onEvent); await new Promise<void>(() => {}) })
    const { container, rerender, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await act(async () => emits[0]!({ event: 'item', item: release(2, '新组', 2) }))
    const groupChip = chip(container, '新组')!
    await act(async () => groupChip.click())
    await act(async () => emits[0]!({ event: 'item', item: release(3, '另一组', 2) }))
    expect(groupChip.getAttribute('aria-pressed')).toBe('true')
    await rerender(<MediaTorrentButton media={{ ...media, id: 9, title: '另一部作品' }} inline />)
    await act(async () => emits[0]!({ event: 'item', item: release(4, '过期组', 2) }))
    expect(chip(container, '过期组')).toBeNull()
    await unmount()
  })

  it('重新搜索后上一次迟到的插件结果不能混进来', async () => {
    const emits: Parameters<typeof streamPluginMagnets>[2][] = []
    vi.mocked(streamPluginMagnets).mockImplementation(async (_q, _c, onEvent) => { emits.push(onEvent); await new Promise<void>(() => {}) })
    vi.mocked(searchMagnets).mockResolvedValue({ query: media.title, sources: [], items: [] })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} inline />)
    await search(container)
    await act(async () => emits[0]!({ event: 'item', item: release(5, '旧组', 3) }))
    expect(chip(container, '旧组')).toBeNull()
    await act(async () => emits[1]!({ event: 'item', item: release(6, '新组', 3) }))
    expect(chip(container, '新组')).not.toBeNull()
    await unmount()
  })

  it('弹窗入口：打开才搜；已经搜过的再打开不重搜', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    expect(searchMagnets).not.toHaveBeenCalled()
    const dialog = dialogOf(container)
    await act(async () => cardEntry(container).click())
    expect(searchMagnets).toHaveBeenCalledOnce()
    expect(rows(container)).toHaveLength(1)
    await act(async () => dialog.close())
    await act(async () => cardEntry(container).click())
    expect(searchMagnets).toHaveBeenCalledOnce()
    await unmount()
  })

  it('没搜完就关窗口：再打开时重新搜，插件来源说明只有一部分', async () => {
    let finishLocal: (() => void) | undefined
    vi.mocked(searchMagnets).mockReturnValueOnce(new Promise(resolve => { finishLocal = () => resolve({ query: media.title, items: [], sources: [] }) }))
    vi.mocked(streamPluginMagnets).mockImplementation(async () => { await new Promise<void>(() => {}) })
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    const dialog = dialogOf(container)
    await act(async () => cardEntry(container).click())
    await act(async () => dialog.close())
    await act(async () => finishLocal!())
    await act(async () => cardEntry(container).click())
    expect(searchMagnets).toHaveBeenCalledTimes(2)
    expect(rows(container)).toHaveLength(1)

    await act(async () => dialog.close())
    await act(async () => cardEntry(container).click())
    expect(container.textContent).toContain('窗口关闭时插件来源还没搜完')
    expect(button(container, '重新搜索插件来源')).toBeDefined()
    await unmount()
  })

  it('搜索失败可重试，弹窗入口保留关闭与播放行为', async () => {
    vi.mocked(searchMagnets).mockRejectedValueOnce(new Error('暂时断线'))
    const { container, unmount } = await mount(<MediaTorrentButton media={media} />)
    await act(async () => cardEntry(container).click())
    expect(dialogOf(container).open).toBe(true)
    const failure = getByRole(container, 'alert', ALL)
    expect(failure.textContent).toContain('暂时断线')
    await act(async () => getByRole(failure, 'button', { name: '重试', ...ALL }).click())
    expect(resources(container)).not.toBeNull()
    await act(async () => playIn(rows(container)[0]!).click())
    expect(dialogOf(container).open).toBe(false)
    await unmount()
  })

  it('作品页的磁力抽屉：「磁力」打开抽屉就搜，行为与弹窗一样；只有按下与松开都在遮罩上才关，关掉后焦点回到「磁力」', async () => {
    const { container, unmount } = await mount(<MediaTorrentButton media={media} drawer />)
    // 抽屉模式没有卡片上的「字幕组」入口
    expect(queryByRole(container, 'button', { name: /^按字幕组查找磁力/, ...ALL })).toBeNull()
    // 入口的名字以可见的「磁力」打头（语音控制按看到的字点得到），后面补一句做什么
    const trigger = getByRole(container, 'button', { name: '磁力：按字幕组查找这部作品的发布' })
    expect(trigger.getAttribute('aria-haspopup')).toBe('dialog')
    const drawer = dialogOf(container)
    // 抽屉的名字：上面的小标题 + 作品名
    expect(accessibleName(drawer)).toBe(`磁力边下边播 ${media.title}`)
    expect(getByRole(drawer, 'heading', { level: 2, ...ALL }).textContent).toBe(media.title)
    await act(async () => trigger.click())
    expect(drawer.open).toBe(true)
    expect(searchMagnets).toHaveBeenCalledOnce()
    expect(rows(container)).toHaveLength(1)
    // 下载整部照常可用
    expect(button(container, '下载')).toBeDefined()

    drawer.getBoundingClientRect = () => ({ left: 600, right: 1000, top: 0, bottom: 800, width: 400, height: 800, x: 600, y: 0, toJSON: () => ({}) })
    const at = (type: string, x: number, target: Element = drawer) =>
      act(async () => { target.dispatchEvent(new MouseEvent(type, { bubbles: true, clientX: x, clientY: 100 })) })
    // 在抽屉里按下、拖到遮罩上松手（拖选文字）：不关
    await at('pointerdown', 700, queryInput(container))
    await at('click', 10)
    expect(drawer.open).toBe(true)
    await at('pointerdown', 10)
    await at('click', 10)
    expect(drawer.open).toBe(false)
    expect(document.activeElement).toBe(trigger)

    // 关闭按钮同样还焦点
    await act(async () => trigger.click())
    await act(async () => getByRole(drawer, 'button', { name: '关闭磁力抽屉' }).click())
    expect(drawer.open).toBe(false)
    expect(document.activeElement).toBe(trigger)
    expect(searchMagnets).toHaveBeenCalledOnce()
    await unmount()
  })

  it('在线选集仍只列已播出的集数', () => {
    expect(airedEpisodeCount({ ...media, episodes: null, status: 'RELEASING', nextAiring: { episode: 5, at: 1 } })).toBe(4)
    expect(airedEpisodeCount({ ...media, episodes: 12, watched: 0, status: 'NOT_YET_RELEASED' })).toBe(0)
  })
})
