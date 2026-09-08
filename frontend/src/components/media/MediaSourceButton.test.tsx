// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import {
  fetchSourcePlugin,
  fetchPlayerStatus,
  playSourceCandidate,
  streamSourceCandidates,
} from '../../lib/endpoints'
import type { SourceCandidate, SourcePluginView } from '../../lib/endpoints'
import { MediaSourceButton } from './MediaSourceButton'
import { SourcePlaybackProvider } from './SourcePlaybackContext'

const shared = vi.hoisted(() => ({ play: vi.fn() }))
vi.mock('../torrent/TorrentPlayContext', () => ({
  useTorrentPlayback: () => ({ state: { phase: 'idle' }, status: null, zeroPeerSeconds: 0, busy: false, play: shared.play, selectFile: vi.fn(), retry: vi.fn(), cancel: vi.fn() }),
}))
vi.mock('../../lib/endpoints', async original => ({
  ...await original<typeof import('../../lib/endpoints')>(),
  fetchPlayerStatus: vi.fn(), fetchSourcePlugin: vi.fn(), playSourceCandidate: vi.fn(), streamSourceCandidates: vi.fn(),
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
const alternate: SourceCandidate = {
  ...online, id: 'online-2', tier: 2,
  transport: { type: 'http', url: 'https://another-secret.invalid/ep2.mp4' },
  metadata: { resolution: '720p' },
}
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')

beforeEach(() => {
  shared.play.mockReset()
  vi.mocked(fetchSourcePlugin).mockReset().mockResolvedValue(plugin)
  vi.mocked(fetchPlayerStatus).mockReset().mockResolvedValue({ playing: true, fileId: 'remote|online-1', title: '测试动画', position: 1, duration: 100, paused: false, danmaku: { state: 'none' } })
  vi.mocked(playSourceCandidate).mockReset().mockImplementation(async candidate => ({ fileId: `remote|${candidate.id}`, title: '测试动画', danmaku: { state: 'none' } }))
  vi.mocked(streamSourceCandidates).mockReset().mockImplementation(async (_request, emit) => {
    emit({ event: 'candidate', candidate: online })
    emit({ event: 'candidate', candidate: alternate })
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
  it('选集后高优先级候选立即起播，列表可换源且不泄露 URL/请求头', async () => {
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
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
    expect(playSourceCandidate).toHaveBeenCalledExactlyOnceWith(online, '测试动画', 2)

    const alternateRow = Array.from(container.querySelectorAll<HTMLLIElement>('.media-resource-list li')).find(row => row.textContent?.includes('720p'))!
    await act(async () => alternateRow.querySelector<HTMLButtonElement>('button')!.click())
    expect(playSourceCandidate).toHaveBeenLastCalledWith(alternate, '测试动画', 2)
    await unmount()
  })

  it('BT 候选复用全局磁力播放会话', async () => {
    const bt: SourceCandidate = { ...online, id: 'bt-1', transport: { type: 'torrent', infoHash: '0123456789abcdef0123456789abcdef01234567', trackers: ['udp://tracker.invalid:80'] } }
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit) => {
      emit({ event: 'candidate', candidate: bt })
      emit({ event: 'done', queried: 1, succeeded: 1, failed: 0, durationMs: 8 })
    })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})
    expect(shared.play).toHaveBeenCalledWith(
      { magnet: expect.stringContaining('magnet:?xt=urn%3Abtih%3A0123456789abcdef'), title: '测试动画', episodeHint: 2, fileIndex: undefined },
      '测试动画',
    )
    expect(playSourceCandidate).not.toHaveBeenCalled()
    await unmount()
  })

  it('首个在线候选立即启动失败时自动尝试下一条', async () => {
    vi.mocked(playSourceCandidate).mockRejectedValueOnce(new Error('上游拒绝连接'))
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})

    expect(playSourceCandidate).toHaveBeenNthCalledWith(1, online, '测试动画', 2)
    expect(playSourceCandidate).toHaveBeenNthCalledWith(2, alternate, '测试动画', 2)
    expect(container.querySelector('.media-source-errors')?.textContent).toContain('上游拒绝连接')
    await unmount()
  })

  it('mpv 启动后异步报播放失败时也会自动换源', async () => {
    vi.mocked(fetchPlayerStatus)
      .mockResolvedValueOnce({ playing: false, playbackFailure: { fileId: 'remote|online-1', reason: '媒体加载失败', at: 1 } })
      .mockResolvedValue({ playing: true, fileId: 'remote|online-2', title: '测试动画', position: 1, duration: 100, paused: false, danmaku: { state: 'none' } })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})
    await act(async () => {})

    expect(playSourceCandidate).toHaveBeenNthCalledWith(1, online, '测试动画', 2)
    expect(playSourceCandidate).toHaveBeenNthCalledWith(2, alternate, '测试动画', 2)
    expect(container.querySelector('.media-source-errors')?.textContent).toContain('媒体加载失败')
    await unmount()
  })
})
