// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { fetchLibrary, playFile } from '../../lib/endpoints'
import type { LibraryCluster, LibraryData, PlayData } from '../../lib/endpoints'
import { MediaPlayButton, nextLibraryEpisode } from './MediaPlayButton'

vi.mock('../../lib/endpoints', () => ({ fetchLibrary: vi.fn(), playFile: vi.fn() }))
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
const started: PlayData = { title: '本地作品', danmaku: { state: 'none' } }
const click = async (container: HTMLElement, selector: string) => act(async () => container.querySelector<HTMLButtonElement>(selector)!.click())
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')

beforeEach(() => {
  vi.mocked(fetchLibrary).mockReset().mockResolvedValue(data)
  vi.mocked(playFile).mockReset().mockResolvedValue(started)
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value) })
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
  it('首次由用户选作品和真实文件，只有播放成功后保存关联', async () => {
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={() => {}} />)
    expect(fetchLibrary).not.toHaveBeenCalled()
    expect(container.querySelector('.discover-card-play')?.textContent).toBe('播放')
    await click(container, '.discover-card-play')
    expect(container.textContent).toContain('没有找到同名')
    expect(playFile).not.toHaveBeenCalled()
    await click(container, '.media-play-search button')
    await click(container, '.media-play-choice')
    expect(localStorage.getItem('nagare:media-library:900')).toBeNull()
    await click(container, '.btn--primary')
    expect(playFile).toHaveBeenCalledExactlyOnceWith('ep2')
    expect(localStorage.getItem('nagare:media-library:900')).toBe('real-cluster')
    expect(container.textContent).toContain('已交给 mpv 播放')
    await unmount()
  })

  it('已有用户关联时读取最新库进度直接续播，不使用榜单的第 8 集', async () => {
    localStorage.setItem('nagare:media-library:900', 'real-cluster')
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={() => {}} />)
    await click(container, '.discover-card-play')
    expect(playFile).toHaveBeenCalledExactlyOnceWith('ep2')
    expect(container.querySelector('.discover-card-play')?.textContent).toBe('继续观看')
    await unmount()
  })

  it('原来的库文件已移除时要求重选，不尝试播放失效文件', async () => {
    localStorage.setItem('nagare:media-library:900', 'removed-cluster')
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={() => {}} />)
    await click(container, '.discover-card-play')
    expect(container.textContent).toContain('已不在媒体库')
    expect(playFile).not.toHaveBeenCalled()
    expect(container.querySelector('.media-play-choice')).not.toBeNull()
    await unmount()
  })

  it('读取期间关闭弹窗不会在稍后自动开始播放', async () => {
    localStorage.setItem('nagare:media-library:900', 'real-cluster')
    let resolve!: (data: LibraryData) => void
    vi.mocked(fetchLibrary).mockReturnValueOnce(new Promise(done => { resolve = done }))
    const changed = vi.fn()
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={changed} />)
    await click(container, '.discover-card-play')
    await click(container, '.media-play-close')
    await act(async () => resolve(data))
    expect(playFile).not.toHaveBeenCalled()
    expect(changed).toHaveBeenLastCalledWith(false)
    expect(document.activeElement).toBe(container.querySelector('.discover-card-play'))
    await unmount()
  })

  it('播放请求在途阻止连点，失败显示错误并允许重试', async () => {
    let reject!: (error: Error) => void
    vi.mocked(playFile).mockReturnValueOnce(new Promise((_, fail) => { reject = fail }))
    const { container, unmount } = await mount(<MediaPlayButton media={media} onOpenChange={() => {}} />)
    await click(container, '.discover-card-play')
    await click(container, '.media-play-search button')
    await click(container, '.media-play-choice')
    await act(async () => {
      const button = container.querySelector<HTMLButtonElement>('.btn--primary')!
      button.click(); button.click()
    })
    expect(playFile).toHaveBeenCalledTimes(1)
    await act(async () => reject(new Error('未找到 mpv')))
    expect(container.querySelector('[role=alert]')?.textContent).toBe('未找到 mpv')
    expect(localStorage.getItem('nagare:media-library:900')).toBeNull()
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
