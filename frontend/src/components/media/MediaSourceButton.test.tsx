// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import {
  fetchSourcePlugin,
  playSourceCandidate,
  streamSourceCandidates,
} from '../../lib/endpoints'
import type { SourceCandidate, SourcePluginView } from '../../lib/endpoints'
import { MediaSourceButton } from './MediaSourceButton'

const shared = vi.hoisted(() => ({ play: vi.fn() }))
vi.mock('../torrent/TorrentPlayContext', () => ({
  useTorrentPlayback: () => ({ state: { phase: 'idle' }, status: null, zeroPeerSeconds: 0, busy: false, play: shared.play, selectFile: vi.fn(), retry: vi.fn(), cancel: vi.fn() }),
}))
vi.mock('../../lib/endpoints', async original => ({
  ...await original<typeof import('../../lib/endpoints')>(),
  fetchSourcePlugin: vi.fn(), playSourceCandidate: vi.fn(), streamSourceCandidates: vi.fn(),
}))

const media = {
  id: 7, title: '测试动画', titleNative: 'テスト', year: 2026,
  episodes: 3, watched: 1, genres: [], status: 'FINISHED',
  episodeTitles: [{ episode: 2, title: '第二话的标题' }],
}
const plugin: SourcePluginView = {
  config: { enabled: true, executable: '/opt/nagare-source', root: '/srv/sources' },
  status: { phase: 'ready', manifest: { id: 'source', name: 'Nagare Source', version: '0.1.0', protocolVersions: [1], sourceSchemaVersions: [1], capabilities: ['candidates'] } },
  sources: [{ id: 'web-a', name: '网页源 A', kind: 'web', tier: 1, version: '1', enabled: true, status: 'ready', capabilities: ['hls'] }],
}
const online: SourceCandidate = {
  schema: 'nagare-candidate/v1', id: 'online-1', sourceId: 'web-a', tier: 1,
  matchConfidence: 0.98, match: { basis: ['title', 'episode'], subjectTitle: '测试动画' },
  transport: { type: 'hls', url: 'https://secret.invalid/play.m3u8', headers: { Cookie: 'private-token' } },
  metadata: { resolution: '1080p', fansub: '字幕组' },
}
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')

beforeEach(() => {
  shared.play.mockReset()
  vi.mocked(fetchSourcePlugin).mockReset().mockResolvedValue(plugin)
  vi.mocked(playSourceCandidate).mockReset().mockResolvedValue({ title: '测试动画', danmaku: { state: 'none' } })
  vi.mocked(streamSourceCandidates).mockReset().mockImplementation(async (_request, emit) => {
    emit({ event: 'candidate', candidate: online })
    emit({ event: 'done', queried: 1, succeeded: 1, failed: 0, durationMs: 8 })
  })
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.open = true } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { if (!this.open) return; this.open = false; this.dispatchEvent(new Event('close')) } })
})

afterEach(() => {
  if (show) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', show); else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal')
  if (close) Object.defineProperty(HTMLDialogElement.prototype, 'close', close); else Reflect.deleteProperty(HTMLDialogElement.prototype, 'close')
})

describe('目录作品的本地插件找源', () => {
  it('选集后显示候选但不泄露 URL/请求头，明确点播放才启动', async () => {
    const { container, unmount } = await mount(<MediaSourceButton media={media} />)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})

    expect(streamSourceCandidates).toHaveBeenCalledWith({
      schema: 'nagare-resolve-request/v1',
      subject: { ids: { anilist: '7' }, titles: ['测试动画', 'テスト'], year: 2026 },
      episode: { number: '2', absolute: 2, title: '第二话的标题' },
      preferences: { preferredTransports: ['hls', 'http', 'torrent'] },
    }, expect.any(Function), expect.any(AbortSignal))
    expect(container.querySelector('.media-resource-list')?.textContent).toContain('1080p')
    expect(container.textContent).not.toContain('secret.invalid')
    expect(container.textContent).not.toContain('private-token')
    expect(playSourceCandidate).not.toHaveBeenCalled()

    await act(async () => container.querySelector<HTMLButtonElement>('.media-resource-list button')!.click())
    expect(playSourceCandidate).toHaveBeenCalledExactlyOnceWith(online, '测试动画', 2)
    await unmount()
  })

  it('BT 候选复用全局磁力播放会话', async () => {
    const bt: SourceCandidate = { ...online, id: 'bt-1', transport: { type: 'torrent', infoHash: '0123456789abcdef0123456789abcdef01234567', trackers: ['udp://tracker.invalid:80'] } }
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit) => {
      emit({ event: 'candidate', candidate: bt })
      emit({ event: 'done', queried: 1, succeeded: 1, failed: 0, durationMs: 8 })
    })
    const { container, unmount } = await mount(<MediaSourceButton media={media} />)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})
    await act(async () => container.querySelector<HTMLButtonElement>('.media-resource-list button')!.click())

    expect(shared.play).toHaveBeenCalledWith(
      { magnet: expect.stringContaining('magnet:?xt=urn%3Abtih%3A0123456789abcdef'), title: '测试动画', episodeHint: 2, fileIndex: undefined },
      '测试动画', expect.any(Function),
    )
    expect(playSourceCandidate).not.toHaveBeenCalled()
    await unmount()
  })
})
