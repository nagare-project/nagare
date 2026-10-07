// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { ApiError } from '../../lib/api'
import { fetchLibrary, playFile, stopPlayer } from '../../lib/endpoints'
import type { LibraryCluster, LibraryData, PlayData } from '../../lib/endpoints'
import { setAssociation } from '../../lib/associations'
import { MediaPlayButton, nextLibraryEpisode } from './MediaPlayButton'

vi.mock('../../lib/endpoints', () => ({ fetchLibrary: vi.fn(), playFile: vi.fn(), stopPlayer: vi.fn() }))
vi.mock('../../lib/associations', async (original) => ({
  ...(await original<typeof import('../../lib/associations')>()),
  setAssociation: vi.fn(),
}))
const media = { id: 900, title: '演示标题', episodes: 12, watched: 8, genres: [] }
const cluster: LibraryCluster = {
  clusterKey: 'real-cluster', title: '本地文件夹名称', season: 1, episodeCount: 3, confidence: 1,
  groups: [{ groupKey: 'main', label: '正片', sortMode: 'episode', items: [
    { fileId: 'opening', fileName: 'NCOP.mkv', episode: null, kind: 'ncop', resolution: null, sizeBytes: 100, progress: null },
    { fileId: 'ep3', fileName: '第三集.mkv', episode: 3, kind: 'main', resolution: '1080p', sizeBytes: 100, progress: null },
    { fileId: 'ep2', fileName: '第二集.mkv', episode: 2, kind: 'main', resolution: '1080p', sizeBytes: 100, progress: { positionSec: 120, durationSec: 1000, completed: false } },
    { fileId: 'ep1', fileName: '第一集.mkv', episode: 1, kind: 'main', resolution: '1080p', sizeBytes: 100, progress: { positionSec: 1000, durationSec: 1000, completed: true } },
  ] }],
}
const data: LibraryData = { clusters: [cluster], folders: [], scannedAt: null, continueWatching: [] }
const associated: LibraryCluster = { ...cluster, association: { mode: 'manual', anilistId: 900, title: '目录里的作品名', setAt: 1 } }
const withClusters = (...clusters: LibraryCluster[]): LibraryData => ({ ...data, clusters })
const legacy = 'nagare:media-library:900'

const started: PlayData = { title: '本地作品', danmaku: { state: 'none' } }
const click = async (container: HTMLElement, selector: string) => act(async () => container.querySelector<HTMLButtonElement>(selector)!.click())
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')

beforeEach(() => {
  vi.mocked(fetchLibrary).mockReset().mockResolvedValue(data)
  vi.mocked(playFile).mockReset().mockResolvedValue(started)
  vi.mocked(stopPlayer).mockReset().mockResolvedValue(undefined)
  vi.mocked(setAssociation).mockReset().mockResolvedValue({ mode: 'manual', anilistId: 900, setAt: 2 })
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
  })
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function () { this.setAttribute('open', '') } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: function () {
    if (!this.open) return
    this.removeAttribute('open'); this.dispatchEvent(new Event('close'))
  } })
})
afterEach(() => {
  vi.unstubAllGlobals()
  if (show) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', show)
  else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal')
  if (close) Object.defineProperty(HTMLDialogElement.prototype, 'close', close)
  else Reflect.deleteProperty(HTMLDialogElement.prototype, 'close')
})

describe('Discover 播放入口', () => {
  it('认定过的本地作品：详情页直接显示真实剧集与媒体库链接，但必须点击才播放', async () => {
    vi.mocked(fetchLibrary).mockResolvedValue(withClusters(associated))
    const { container, unmount } = await mount(<MediaPlayButton media={media} inline />)
    expect(fetchLibrary).toHaveBeenCalledOnce()
    expect(container.querySelector('dialog')).toBeNull()
    expect(container.querySelectorAll('.episode-card')).toHaveLength(4)
    expect(container.querySelector('.episode-feature-meta')?.textContent).toContain('第 02 集 / 12')
    expect(container.querySelector('.media-play-selection h3')?.textContent).toBe('目录里的作品名')
    const back = [...container.querySelectorAll('a')].find(a => a.textContent === '在媒体库中打开')
    expect(back?.getAttribute('href')).toBe('/anime/real-cluster')
    expect(playFile).not.toHaveBeenCalled()
    await click(container, '.episode-feature')
    expect(playFile).toHaveBeenCalledExactlyOnceWith('ep2')
    expect(setAssociation).not.toHaveBeenCalled()
    await unmount()
  })

  it('以前只记在浏览器里的选择：打开时同步到后端并删掉旧记录', async () => {
    localStorage.setItem(legacy, cluster.clusterKey)
    const { container, unmount } = await mount(<MediaPlayButton media={media} inline />)
    expect(setAssociation).toHaveBeenCalledExactlyOnceWith('real-cluster', { mode: 'manual', anilistId: 900 })
    expect(localStorage.getItem(legacy)).toBeNull()
    expect(container.querySelectorAll('.episode-card')).toHaveLength(4)
    await click(container, '.episode-feature')
    expect(setAssociation).toHaveBeenCalledOnce()
    await unmount()
  })

  it('旧记录同步失败：照旧可以播，说一声，旧记录留着下次再试', async () => {
    localStorage.setItem(legacy, cluster.clusterKey)
    vi.mocked(setAssociation).mockRejectedValueOnce(new Error('作品目录暂不可用'))
    const { container, unmount } = await mount(<MediaPlayButton media={media} inline />)
    expect(container.textContent).toContain('以前在这个浏览器里选的本地作品还没能同步到媒体库（作品目录暂不可用）')
    expect(localStorage.getItem(legacy)).toBe('real-cluster')
    expect(container.querySelectorAll('.episode-card')).toHaveLength(4)
    await unmount()
  })

  it('那个分组在后端已经有了别的决定：旧记录作废，不再采用', async () => {
    localStorage.setItem(legacy, cluster.clusterKey)
    vi.mocked(fetchLibrary).mockResolvedValue(withClusters({ ...cluster, association: { mode: 'manual', anilistId: 1, title: '别的作品', setAt: 1 } }))
    const { container, unmount } = await mount(<MediaPlayButton media={{ ...media, title: cluster.title }} inline />)
    expect(localStorage.getItem(legacy)).toBeNull()
    expect(setAssociation).not.toHaveBeenCalled()
    expect(container.querySelector('.episode-feature')).toBeNull()
    // 列表里能看到它对应了谁，但不能在这里改，要去它的作品页
    expect(container.querySelector('.media-play-choice--taken')?.textContent).toContain('已对应「别的作品」')
    expect(container.querySelector('.media-play-choice--taken a')?.getAttribute('href')).toBe('/anime/real-cluster')
    expect(container.querySelector('button.media-play-choice')).toBeNull()
    await unmount()
  })

  it('详情页未关联作品时由用户挑，不按名称自动关联；挑了只是看，点播放才认定', async () => {
    const { container, unmount } = await mount(<MediaPlayButton media={{ ...media, title: cluster.title }} inline />)
    expect(container.querySelector('.media-play-choice')).not.toBeNull()
    expect(container.querySelector('.episode-feature')).toBeNull()
    await click(container, '.media-play-choice')
    expect(container.querySelectorAll('.episode-card')).toHaveLength(4)
    expect(setAssociation).not.toHaveBeenCalled()
    expect(playFile).not.toHaveBeenCalled()
    await unmount()
  })

  it('首次由用户选作品和真实文件：点播放时先认定，认定落地之后才播放', async () => {
    let saved!: () => void
    vi.mocked(setAssociation).mockReturnValueOnce(new Promise(done => { saved = () => done({ mode: 'manual', anilistId: 900, setAt: 2 }) }))
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={() => {}} />)
    expect(fetchLibrary).not.toHaveBeenCalled()
    expect(container.querySelector('.discover-card-play')?.textContent).toBe('播放')
    await click(container, '.discover-card-play')
    expect(container.textContent).toContain('没有找到同名')
    expect(playFile).not.toHaveBeenCalled()
    await click(container, '.media-play-search button')
    await click(container, '.media-play-choice')
    await click(container, '.btn--primary')
    expect(setAssociation).toHaveBeenCalledExactlyOnceWith('real-cluster', { mode: 'manual', anilistId: 900 })
    expect(playFile).not.toHaveBeenCalled()
    await act(async () => saved())
    expect(playFile).toHaveBeenCalledExactlyOnceWith('ep2')
    expect(localStorage.getItem(legacy)).toBeNull()
    expect(container.textContent).toContain('已交给 mpv 播放')
    await unmount()
  })

  it('认定失败不挡播放：照常播，并说明这一集会按文件名自动匹配', async () => {
    vi.mocked(setAssociation).mockRejectedValueOnce(new Error('作品目录暂不可用'))
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={() => {}} />)
    await click(container, '.discover-card-play')
    await click(container, '.media-play-search button')
    await click(container, '.media-play-choice')
    await click(container, '.btn--primary')
    expect(playFile).toHaveBeenCalledExactlyOnceWith('ep2')
    expect(container.textContent).toContain('已交给 mpv 播放')
    expect(container.textContent).toContain('没能记住这部作品对应的本地文件夹（作品目录暂不可用）。这一集仍按文件名自动匹配')
    expect(stopPlayer).not.toHaveBeenCalled()
    await unmount()
  })

  it('因为这个分组正在播放而被拒（409）：先停掉当前播放再认定一次，然后播放', async () => {
    vi.mocked(setAssociation).mockRejectedValueOnce(new ApiError('正在播放这部作品，停止播放后再改', 409))
    const { container, unmount } = await mount(<MediaPlayButton media={{ ...media, title: cluster.title }} inline />)
    await click(container, '.media-play-choice')
    await click(container, '.episode-feature')
    expect(stopPlayer).toHaveBeenCalledOnce()
    expect(setAssociation).toHaveBeenCalledTimes(2)
    expect(playFile).toHaveBeenCalledExactlyOnceWith('ep2')
    expect(container.textContent).not.toContain('没能记住')
    await unmount()
  })

  it('认定还没回来就关掉了弹窗：不再启动播放', async () => {
    let saved!: () => void
    vi.mocked(setAssociation).mockReturnValueOnce(new Promise(done => { saved = () => done({ mode: 'manual', anilistId: 900, setAt: 2 }) }))
    const { container, unmount } = await mount(<MediaPlayButton media={{ ...media, title: cluster.title }} onOpenChange={() => {}} />)
    await click(container, '.discover-card-play')
    await click(container, '.media-play-choice')
    await click(container, '.btn--primary')
    await click(container, '.media-play-close')
    await act(async () => saved())
    expect(playFile).not.toHaveBeenCalled()
    await unmount()
  })

  it('旧记录同步失败又要自动播放：不再等第二轮认定，直接播', async () => {
    localStorage.setItem(legacy, cluster.clusterKey)
    vi.mocked(setAssociation).mockRejectedValue(new Error('作品目录暂不可用'))
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={() => {}} />)
    await click(container, '.discover-card-play')
    expect(setAssociation).toHaveBeenCalledOnce()
    expect(playFile).toHaveBeenCalledExactlyOnceWith('ep2')
    await unmount()
  })

  it('认定成功、播放失败：只报播放的错，不再说「没能记住」', async () => {
    vi.mocked(playFile).mockRejectedValueOnce(new Error('未找到 mpv'))
    const { container, unmount } = await mount(<MediaPlayButton media={{ ...media, title: cluster.title }} inline />)
    await click(container, '.media-play-choice')
    await click(container, '.episode-feature')
    expect(container.querySelector('[role=alert]')?.textContent).toBe('未找到 mpv')
    expect(container.textContent).not.toContain('没能记住')
    await unmount()
  })

  it('没认过的本地作品在列表里说出自动匹配到了谁；认定的作品名也能搜到', async () => {
    const s2: LibraryCluster = { ...cluster, clusterKey: 's2', title: 'Frieren', matched: { anilistId: 7, title: '第二季' } }
    const mine: LibraryCluster = { ...cluster, clusterKey: 'mine', title: 'Frieren', association: { mode: 'manual', anilistId: 900, title: '芙莉莲', setAt: 1 } }
    vi.mocked(fetchLibrary).mockResolvedValue(withClusters(s2, mine, { ...mine, clusterKey: 'mine2' }))
    const { container, unmount } = await mount(<MediaPlayButton media={{ ...media, title: 'Frieren' }} inline />)
    const rows = [...container.querySelectorAll('.media-play-choice')].map(el => el.textContent)
    expect(rows.find(text => text?.includes('自动匹配到「第二季」'))).toBeTruthy()
    const search = container.querySelector<HTMLInputElement>('.media-play-search input')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search, '芙莉莲')
      search.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.querySelectorAll('.media-play-choice')).toHaveLength(2)
    await unmount()
  })

  it('认定过时读取最新库进度直接续播，不使用榜单的第 8 集', async () => {
    vi.mocked(fetchLibrary).mockResolvedValue(withClusters(associated))
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={() => {}} />)
    await click(container, '.discover-card-play')
    expect(playFile).toHaveBeenCalledExactlyOnceWith('ep2')
    expect(container.querySelector('.discover-card-play')?.textContent).toBe('继续观看')
    await unmount()
  })

  it('自动匹配到的本地作品只代选，不自动播放（用户没确认过）', async () => {
    vi.mocked(fetchLibrary).mockResolvedValue(withClusters({ ...cluster, matched: { anilistId: 900, title: '目录里的作品名' } }))
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={() => {}} />)
    await click(container, '.discover-card-play')
    expect(playFile).not.toHaveBeenCalled()
    expect(container.querySelector('.media-play-selection')?.textContent).toContain('自动匹配到的本地作品')
    await click(container, '.btn--primary')
    expect(setAssociation).toHaveBeenCalledExactlyOnceWith('real-cluster', { mode: 'manual', anilistId: 900 })
    expect(playFile).toHaveBeenCalledExactlyOnceWith('ep2')
    await unmount()
  })

  it('同一部作品认定了不止一个本地分组：让用户挑，已认定的排在前面', async () => {
    const other: LibraryCluster = { ...associated, clusterKey: 'bd-cluster', title: 'BD 版文件夹' }
    const unrelated: LibraryCluster = { ...cluster, clusterKey: 'unrelated', title: '无关的番' }
    vi.mocked(fetchLibrary).mockResolvedValue(withClusters(unrelated, associated, other))
    const { container, unmount } = await mount(<MediaPlayButton media={media} inline />)
    expect(container.querySelector('.episode-feature')).toBeNull()
    const choices = [...container.querySelectorAll('.media-play-choice')].map(el => el.textContent)
    expect(choices.slice(0, 2).every(text => text?.includes('已对应这部作品'))).toBe(true)
    expect(choices[2]).toContain('无关的番')
    await unmount()
  })

  it('原来的库文件已移除时要求重选，不尝试播放失效文件', async () => {
    localStorage.setItem(legacy, 'removed-cluster')
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={() => {}} />)
    await click(container, '.discover-card-play')
    expect(container.textContent).toContain('已不在媒体库')
    expect(playFile).not.toHaveBeenCalled()
    expect(container.querySelector('.media-play-choice')).not.toBeNull()
    await unmount()
  })

  it('读取期间关闭弹窗不会在稍后自动开始播放', async () => {
    let resolve!: (data: LibraryData) => void
    vi.mocked(fetchLibrary).mockReturnValueOnce(new Promise(done => { resolve = done }))
    const changed = vi.fn()
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={changed} />)
    await click(container, '.discover-card-play')
    await click(container, '.media-play-close')
    await act(async () => resolve(withClusters(associated)))
    expect(playFile).not.toHaveBeenCalled()
    expect(changed).toHaveBeenLastCalledWith(false)
    expect(document.activeElement).toBe(container.querySelector('.discover-card-play'))
    await unmount()
  })

  it('播放请求在途阻止连点，失败显示错误并允许重试', async () => {
    let reject!: (error: Error) => void
    vi.mocked(playFile).mockReturnValueOnce(new Promise((_, fail) => { reject = fail }))
    vi.mocked(fetchLibrary).mockResolvedValue(withClusters(associated))
    const { container, unmount } = await mount(<MediaPlayButton media={media} inline />)
    await act(async () => {
      const button = container.querySelector<HTMLButtonElement>('.btn--primary')!
      button.click(); button.click()
    })
    expect(playFile).toHaveBeenCalledTimes(1)
    await act(async () => reject(new Error('未找到 mpv')))
    expect(container.querySelector('[role=alert]')?.textContent).toBe('未找到 mpv')
    await click(container, '.btn--primary')
    expect(playFile).toHaveBeenCalledTimes(2)
    expect(container.textContent).toContain('已交给 mpv 播放')
    await unmount()
  })

  it('自动续播跳过片头和花絮，并在无进度时按实际集号播放', () => {
    expect(nextLibraryEpisode(cluster)?.fileId).toBe('ep2')
    const fresh = { ...cluster, groups: cluster.groups.map(group => ({ ...group, items: group.items.map(item => ({ ...item, progress: null })) })) }
    expect(nextLibraryEpisode(fresh)?.fileId).toBe('ep1')
    expect(nextLibraryEpisode({ ...cluster, groups: [{ ...cluster.groups[0]!, items: [cluster.groups[0]!.items[0]!] }] })).toBeUndefined()
  })
})
