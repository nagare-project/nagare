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
import type { TorrentPlayState } from '../../hooks/useTorrentPlay'
import { MediaSourceButton } from './MediaSourceButton'
import { SourcePlaybackProvider } from './SourcePlaybackContext'

// 磁力播放会话的替身：测试改 shared.torrent 再 rerender，就是状态条那份会话往前走了一步
const shared = vi.hoisted(() => ({ play: vi.fn(), torrent: { state: { phase: 'idle' } as TorrentPlayState, zeroPeerSeconds: 0 } }))
vi.mock('../torrent/TorrentPlayContext', () => ({
  useTorrentPlayback: () => ({ state: shared.torrent.state, status: null, zeroPeerSeconds: shared.torrent.zeroPeerSeconds, busy: false, play: shared.play, selectFile: vi.fn(), retry: vi.fn(), cancel: vi.fn() }),
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
  sources: [{ id: 'web-a', name: '网页源 A', kind: 'web', tier: 1, version: '1', enabled: true, status: 'healthy', capabilities: ['hls'] }],
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
const button = (container: HTMLElement, text: string) => [...container.querySelectorAll<HTMLButtonElement>('button')].find(item => item.textContent?.includes(text))
const kind = (container: HTMLElement, label: '在线' | 'BT') => [...container.querySelectorAll<HTMLButtonElement>('.source-kind')].find(item => item.textContent?.startsWith(label))
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')

beforeEach(() => {
  shared.play.mockReset()
  shared.torrent = { state: { phase: 'idle' }, zeroPeerSeconds: 0 }
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
  it('详情页只展示已播出剧集，切换列表和网格不会自动找源', async () => {
    const dated = { ...media, episodeTitles: [
      { episode: 1, title: '已播出', airedAt: '2020-01-01T00:00:00Z', image: '/one.jpg' },
      { episode: 2, title: '未来集', airedAt: '2099-01-01T00:00:00Z' },
    ] }
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={dated} inline /></SourcePlaybackProvider>)
    expect(container.querySelectorAll('.media-episode-button')).toHaveLength(1)
    expect(container.querySelector('.media-episode-grid--list')).not.toBeNull()
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="切换网格视图"]')!.click())
    expect(container.querySelector('.media-episode-grid--list')).toBeNull()
    expect(container.querySelector('.media-source-still img')?.getAttribute('src')).toBe('/one.jpg')
    expect(streamSourceCandidates).not.toHaveBeenCalled()
    await unmount()
  })

  it('快速切集后忽略上一集迟到的候选，避免自动播错集', async () => {
    const streams: Array<{ emit: Parameters<typeof streamSourceCandidates>[1]; signal?: AbortSignal; finish: () => void }> = []
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit, signal) => {
      await new Promise<void>(finish => streams.push({ emit, signal, finish }))
    })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 1 集"]')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    expect(streams[0]?.signal?.aborted).toBe(true)
    await act(async () => streams[0]!.emit({ event: 'candidate', candidate: { ...online, id: 'old-episode' } }))
    expect(playSourceCandidate).not.toHaveBeenCalled()
    await act(async () => streams[1]!.emit({ event: 'candidate', candidate: online }))
    expect(playSourceCandidate).toHaveBeenCalledExactlyOnceWith(online, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })
    await act(async () => streams.forEach(stream => stream.finish()))
    await unmount()
  })

  it('起播标题用目录作品标题而不是来源站点的写法，保证弹幕关键词匹配', async () => {
    const renamed = { ...online, match: { ...online.match, subjectTitle: '幼女战记 第二季' } }
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit) => {
      emit({ event: 'candidate', candidate: renamed })
    })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    expect(playSourceCandidate).toHaveBeenCalledExactlyOnceWith(renamed, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })
    await unmount()
  })

  it('用户正常关闭播放器后不自动启动另一条来源', async () => {
    vi.mocked(fetchPlayerStatus).mockResolvedValue({ playing: false })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})
    expect(playSourceCandidate).toHaveBeenCalledTimes(1)
    await unmount()
  })

  it('mpv 打开后立即停止找源：关掉 mpv 后迟到的候选不会再打开新的播放窗口', async () => {
    let emitEvent: Parameters<typeof streamSourceCandidates>[1] | undefined
    let streamSignal: AbortSignal | undefined
    let finish: (() => void) | undefined
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit, signal) => {
      emitEvent = emit
      streamSignal = signal
      emit({ event: 'candidate', candidate: online })
      await new Promise<void>(resolve => { finish = resolve })
    })
    vi.mocked(fetchPlayerStatus).mockResolvedValue({ playing: false })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})
    expect(playSourceCandidate).toHaveBeenCalledTimes(1)
    expect(streamSignal?.aborted).toBe(true)
    expect(button(container, '停止继续找源')).toBeUndefined()

    await act(async () => emitEvent!({ event: 'candidate', candidate: alternate }))
    await act(async () => emitEvent!({ event: 'done', queried: 1, succeeded: 1, failed: 0, durationMs: 8 }))
    expect(playSourceCandidate).toHaveBeenCalledTimes(1)
    await act(async () => finish!())
    await unmount()
  })

  it('更高优先级来源失败后立即播放已有在线候选，不等待整个流结束', async () => {
    const lower = { ...online, id: 'lower', sourceId: 'web-b', tier: 2 }
    vi.mocked(fetchSourcePlugin).mockResolvedValue({ ...plugin, sources: [...plugin.sources,
      { ...plugin.sources[0]!, id: 'web-b', tier: 2 },
    ] })
    let emitEvent: Parameters<typeof streamSourceCandidates>[1] | undefined
    let finish: (() => void) | undefined
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit) => {
      emitEvent = emit
      emit({ event: 'candidate', candidate: lower })
      await new Promise<void>(resolve => { finish = resolve })
    })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    expect(playSourceCandidate).not.toHaveBeenCalled()
    await act(async () => emitEvent!({ event: 'source_error', sourceId: 'web-a', category: 'search_failed', message: 'failed', retryable: true }))
    expect(playSourceCandidate).toHaveBeenCalledExactlyOnceWith(lower, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })
    await act(async () => finish!())
    await unmount()
  })

  it('已启用来源没有更高优先级时首条可靠在线候选立即起播', async () => {
    const lower = { ...online, tier: 2 }
    vi.mocked(fetchSourcePlugin).mockResolvedValue({ ...plugin, sources: [{ ...plugin.sources[0]!, tier: 2 }] })
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit) => {
      emit({ event: 'candidate', candidate: lower })
      expect(playSourceCandidate).toHaveBeenCalledExactlyOnceWith(lower, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })
    })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await unmount()
  })

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
    expect(playSourceCandidate).toHaveBeenCalledExactlyOnceWith(online, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })

    const alternateRow = Array.from(container.querySelectorAll<HTMLLIElement>('.media-resource-list li')).find(row => row.textContent?.includes('720p'))!
    await act(async () => alternateRow.querySelector<HTMLButtonElement>('button')!.click())
    expect(playSourceCandidate).toHaveBeenLastCalledWith(alternate, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })
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
      { magnet: expect.stringContaining('magnet:?xt=urn%3Abtih%3A0123456789abcdef'), title: '测试动画', episodeHint: 2, suggestedFileIndex: undefined, anilistId: 7, titles: ['测试动画', 'テスト'] },
      '测试动画',
    )
    expect(playSourceCandidate).not.toHaveBeenCalled()
    await unmount()
  })

  it('回退到 BT 时先挑有中文字幕的那条，列表自动切到 BT', async () => {
    const english: SourceCandidate = { ...online, id: 'bt-en', tier: 3, transport: { type: 'torrent', infoHash: 'e'.repeat(40) }, metadata: { resolution: '1080p', fansub: 'SubsPlease', subtitleLanguages: ['en'], seeders: 900 } }
    const chinese: SourceCandidate = { ...online, id: 'bt-zh', tier: 3, transport: { type: 'torrent', infoHash: 'c'.repeat(40) }, metadata: { resolution: '1080p', fansub: 'LoliHouse', title: '[LoliHouse] 测试动画 - 02 [简繁内封字幕]', seeders: 3 } }
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit) => {
      emit({ event: 'candidate', candidate: english })
      emit({ event: 'candidate', candidate: chinese })
      emit({ event: 'done', queried: 1, succeeded: 1, failed: 0, durationMs: 8 })
    })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})
    expect(shared.play).toHaveBeenCalledOnce()
    expect(shared.play.mock.calls[0]?.[0].magnet).toContain('c'.repeat(40))
    expect(kind(container, 'BT')?.getAttribute('aria-pressed')).toBe('true')
    const names = [...container.querySelectorAll('.source-candidate-name')].map(node => node.textContent)
    expect(names).toEqual(['LoliHouse', 'SubsPlease'])
    expect(container.querySelector('.media-resource-list li')?.textContent).toContain('中字')
    await unmount()
  })

  it('候选按在线 / BT 分开，收起时每类只列前几条，报错的来源折叠成一行', async () => {
    const bts: SourceCandidate[] = Array.from({ length: 7 }, (_, index) => ({ ...online, id: `bt-${index}`, tier: 3,
      transport: { type: 'torrent', infoHash: String(index).repeat(40) }, metadata: { fansub: `组${index}`, seeders: 10 - index } }))
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit) => {
      emit({ event: 'source_error', sourceId: 'web-b', category: 'resolve_timeout', message: 'timeout', retryable: true })
      emit({ event: 'candidate', candidate: online })
      for (const candidate of bts) emit({ event: 'candidate', candidate })
      emit({ event: 'done', queried: 2, succeeded: 1, failed: 1, durationMs: 8 })
    })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})

    expect(container.querySelector('.source-issues summary')?.textContent).toBe('1 个来源没能用上')
    expect(container.querySelector('.source-issues')?.hasAttribute('open')).toBe(false)
    expect(container.querySelector('.media-source-errors')?.textContent).toContain('解析播放地址超时')
    expect(kind(container, '在线')?.getAttribute('aria-pressed')).toBe('true')
    expect(container.querySelectorAll('.media-resource-list li')).toHaveLength(1)

    await act(async () => kind(container, 'BT')!.click())
    expect(container.querySelectorAll('.media-resource-list li')).toHaveLength(4)
    await act(async () => button(container, '显示全部 7 条')!.click())
    expect(container.querySelectorAll('.media-resource-list li')).toHaveLength(7)
    await act(async () => button(container, '收起')!.click())
    expect(container.querySelectorAll('.media-resource-list li')).toHaveLength(4)
    await unmount()
  })

  it('只有 .torrent 地址的 BT 候选也交给全局播放会话', async () => {
    const bt: SourceCandidate = { ...online, id: 'bt-url', transport: { type: 'torrent', torrentUrl: 'https://tracker.invalid/release.torrent' } }
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit) => {
      emit({ event: 'candidate', candidate: bt })
      emit({ event: 'done', queried: 1, succeeded: 1, failed: 0, durationMs: 8 })
    })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})
    expect(shared.play).toHaveBeenCalledWith(
      { torrentUrl: 'https://tracker.invalid/release.torrent', title: '测试动画', episodeHint: 2, suggestedFileIndex: undefined, anilistId: 7, titles: ['测试动画', 'テスト'] },
      '测试动画',
    )
    await unmount()
  })

  it('首个在线候选立即启动失败时自动尝试下一条', async () => {
    vi.mocked(playSourceCandidate).mockRejectedValueOnce(new Error('上游拒绝连接'))
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})

    expect(playSourceCandidate).toHaveBeenNthCalledWith(1, online, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })
    expect(playSourceCandidate).toHaveBeenNthCalledWith(2, alternate, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })
    expect(container.querySelector('.media-source-errors')?.textContent).toContain('上游拒绝连接')
    await unmount()
  })

  it('mpv 打开后报播放失败：不再自动打开下一条，说明原因并留给用户手动换', async () => {
    vi.mocked(fetchPlayerStatus)
      .mockResolvedValue({ playing: false, playbackFailure: { fileId: 'remote|online-1', reason: '媒体加载失败', at: 1 } })
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})
    await act(async () => {})

    expect(playSourceCandidate).toHaveBeenCalledExactlyOnceWith(online, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })
    expect(container.querySelector('.media-source-errors')?.textContent).toContain('媒体加载失败')
    expect(container.textContent).toContain('不会再自动打开新的播放窗口')

    const alternateRow = Array.from(container.querySelectorAll<HTMLLIElement>('.media-resource-list li')).find(row => row.textContent?.includes('720p'))!
    await act(async () => alternateRow.querySelector<HTMLButtonElement>('button')!.click())
    expect(playSourceCandidate).toHaveBeenLastCalledWith(alternate, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })
    await unmount()
  })

  it('手动点的候选启动失败：只提示，不替用户接着试下一条', async () => {
    const { container, unmount } = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})
    vi.mocked(playSourceCandidate).mockClear().mockRejectedValueOnce(new Error('上游拒绝连接'))

    const alternateRow = Array.from(container.querySelectorAll<HTMLLIElement>('.media-resource-list li')).find(row => row.textContent?.includes('720p'))!
    await act(async () => alternateRow.querySelector<HTMLButtonElement>('button')!.click())
    expect(playSourceCandidate).toHaveBeenCalledExactlyOnceWith(alternate, '测试动画', 2, { anilistId: 7, altTitles: ['テスト'] })
    expect(container.textContent).toContain('这条候选启动失败，可在列表中换一条')
    await unmount()
  })
})

describe('BT 回退：mpv 打开前没人分享、起播失败就换下一条', () => {
  const bt = (id: string, hash: string, extra: Partial<SourceCandidate['metadata']> = {}): SourceCandidate => ({
    ...online, id, tier: 3, transport: { type: 'torrent', infoHash: hash.repeat(40) }, metadata: { fansub: `组${id}`, ...extra },
  })
  const onlyBT = (candidates: SourceCandidate[]) => vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit) => {
    for (const candidate of candidates) emit({ event: 'candidate', candidate })
    emit({ event: 'done', queried: 1, succeeded: 1, failed: 0, durationMs: 8 })
  })
  const started = (index: number): string => shared.play.mock.calls[index]![0].magnet as string
  async function start(): Promise<Awaited<ReturnType<typeof mount>>> {
    const mounted = await mount(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
    await act(async () => mounted.container.querySelector<HTMLButtonElement>('.discover-card-source')!.click())
    await act(async () => mounted.container.querySelector<HTMLButtonElement>('[aria-label="从本地插件查找第 2 集"]')!.click())
    await act(async () => {})
    return mounted
  }
  const step = async (mounted: Awaited<ReturnType<typeof mount>>, state: TorrentPlayState, zeroPeerSeconds = 0) => {
    shared.torrent = { state, zeroPeerSeconds }
    await mounted.rerender(<SourcePlaybackProvider><MediaSourceButton media={media} /></SourcePlaybackProvider>)
  }

  it('15 秒连不上分享者就换下一条；不同来源报的同一个种子不重复试', async () => {
    onlyBT([bt('a', 'a', { seeders: 9 }), { ...bt('a-dup', 'a', { seeders: 8 }), sourceId: 'mikan' }, bt('b', 'b', { seeders: 1 })])
    const mounted = await start()
    expect(shared.play).toHaveBeenCalledOnce()
    expect(started(0)).toContain('a'.repeat(40))

    await step(mounted, { phase: 'starting', magnet: started(0), title: '测试动画' }, 10)
    expect(shared.play).toHaveBeenCalledOnce()
    await step(mounted, { phase: 'starting', magnet: started(0), title: '测试动画' }, 15)
    expect(shared.play).toHaveBeenCalledTimes(2)
    expect(started(1)).toContain('b'.repeat(40))
    expect(mounted.container.querySelector('.media-source-errors')?.textContent).toContain('15 秒内没有连接到任何分享者')
    await mounted.unmount()
  })

  it('起播失败也换下一条；最后一条没人分享就接着等，不再换', async () => {
    onlyBT([bt('a', 'a', { seeders: 9 }), bt('b', 'b', { seeders: 1 })])
    const mounted = await start()
    await step(mounted, { phase: 'error', magnet: started(0), title: '测试动画', message: '缓冲超时，分享者太少' })
    expect(shared.play).toHaveBeenCalledTimes(2)
    expect(mounted.container.querySelector('.media-source-errors')?.textContent).toContain('缓冲超时，分享者太少')

    await step(mounted, { phase: 'starting', magnet: started(1), title: '测试动画' }, 30)
    expect(shared.play).toHaveBeenCalledTimes(2)
    expect(mounted.container.textContent).toContain('这是最后一条 BT 候选，继续等分享者')
    await step(mounted, { phase: 'error', magnet: started(1), title: '测试动画', message: '找不到可用的分享者' })
    expect(shared.play).toHaveBeenCalledTimes(2)
    expect(mounted.container.textContent).toContain('所有 BT 候选都没能起播')
    await mounted.unmount()
  })

  it('mpv 打开以后（或要用户挑文件时）不再自动换', async () => {
    onlyBT([bt('a', 'a', { seeders: 9 }), bt('b', 'b', { seeders: 1 })])
    const mounted = await start()
    await step(mounted, { phase: 'streaming', magnet: started(0), title: '测试动画', danmaku: { state: 'none' } })
    await step(mounted, { phase: 'error', magnet: started(0), title: '测试动画', message: '磁力流中断' })
    expect(shared.play).toHaveBeenCalledOnce()
    await mounted.unmount()
  })

  it('用户在状态条取消：不再自动换，列表里可以手动换', async () => {
    onlyBT([bt('a', 'a', { seeders: 9 }), bt('b', 'b', { seeders: 1 })])
    const mounted = await start()
    await step(mounted, { phase: 'idle' })
    await step(mounted, { phase: 'idle' }, 20)
    expect(shared.play).toHaveBeenCalledOnce()
    expect(mounted.container.textContent).toContain('磁力播放已停止')
    await mounted.unmount()
  })

  it('手动点的 BT 没人分享：只提示，不替用户换', async () => {
    vi.mocked(fetchPlayerStatus).mockResolvedValue({ playing: true, fileId: 'remote|online-1', title: '测试动画', position: 1, duration: 100, paused: false, danmaku: { state: 'none' } })
    vi.mocked(streamSourceCandidates).mockImplementation(async (_request, emit) => {
      emit({ event: 'candidate', candidate: online })
      emit({ event: 'candidate', candidate: bt('a', 'a', { seeders: 9 }) })
      emit({ event: 'candidate', candidate: bt('b', 'b', { seeders: 1 }) })
      emit({ event: 'done', queried: 1, succeeded: 1, failed: 0, durationMs: 8 })
    })
    const mounted = await start()
    expect(playSourceCandidate).toHaveBeenCalledOnce()
    await act(async () => [...mounted.container.querySelectorAll<HTMLButtonElement>('.source-kind')].find(item => item.textContent?.startsWith('BT'))!.click())
    await act(async () => mounted.container.querySelector<HTMLButtonElement>('.media-resource-list li button')!.click())
    expect(shared.play).toHaveBeenCalledOnce()
    await step(mounted, { phase: 'starting', magnet: started(0), title: '测试动画' }, 15)
    expect(shared.play).toHaveBeenCalledOnce()
    expect(mounted.container.textContent).toContain('这条 BT 没能起播，可以在 BT 栏换一条')
    await mounted.unmount()
  })
})
