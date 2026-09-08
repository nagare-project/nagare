// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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
    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith('测试动画')
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

  it('未知总集数只列已有放送，未播作品不虚构集表', () => {
    expect(airedEpisodeCount({ ...media, episodes: null, status: 'RELEASING', nextAiring: { episode: 5, at: 1 } })).toBe(4)
    expect(airedEpisodeCount({ ...media, episodes: 12, watched: 0, status: 'NOT_YET_RELEASED' })).toBe(0)
    expect(airedEpisodeCount({ ...media, format: 'MOVIE', episodes: null })).toBe(1)
  })
})
