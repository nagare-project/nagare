// @vitest-environment jsdom
// 搜索页冒烟：真实路由（memory history）+ mock 掉 endpoints 层。
// 重点验三种空态与「源异常 ≠ 无结果」（决议 CQ3 的验收：规则改坏时界面显示「源异常」）。
import { act } from 'react'
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type {
  SearchResult,
  SettingsData,
  SourceOutcome,
  SourcesData,
  TorrentPlayData,
} from '../lib/endpoints'
import {
  fetchSettings,
  fetchSources,
  fetchTorrentStatus,
  fetchUpdate,
  searchMagnets,
  setSourceEnabled,
  startTorrentPlay,
  stopTorrent,
} from '../lib/endpoints'
import { createAppRouter } from '../routes'
import { mount } from '../test/harness'
import { installLocalStorage } from '../test/storage'

vi.mock('../lib/endpoints', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/endpoints')>()),
  fetchSettings: vi.fn(),
  fetchSources: vi.fn(),
  fetchTorrentStatus: vi.fn(),
  fetchUpdate: vi.fn(),
  startTorrentPlay: vi.fn(),
  stopTorrent: vi.fn(),
  searchMagnets: vi.fn(),
  setSourceEnabled: vi.fn(),
}))

const RULES: SourcesData['rules'] = {
  remoteUrl: 'https://example.invalid/rules',
  localDir: '',
  dir: '/tmp/rules',
  loaded: 2,
  errors: [],
  lastLoadedAt: 1_756_500_000,
  lastSyncAt: null,
}

const TWO_SOURCES: SourcesData = {
  sources: [
    {
      id: 'src-a',
      name: '源甲',
      homepage: 'https://a.invalid',
      enabled: true,
      capabilities: { seeders: true, priority: 1 },
      hasSelfTest: true,
    },
    {
      id: 'src-b',
      name: '源乙',
      homepage: '',
      enabled: true,
      capabilities: { seeders: false, priority: 2 },
      hasSelfTest: false,
    },
  ],
  rules: RULES,
}

const SETTINGS: SettingsData = {
  version: '0.1.0',
  platform: 'darwin',
  arch: 'arm64',
  dataDir: '/tmp/nagare',
  logPath: '/tmp/nagare/nagare.log',
  mpv: { found: true, version: '0.38.0', path: '/opt/homebrew/bin/mpv', source: 'path' },
  animego: { loggedIn: false, baseUrl: 'https://animego.example' },
  torrent: {
    enabled: true,
    seeding: false,
    trackers: [],
    portForwarding: true,
    listenPort: 6881,
    cacheDir: '/tmp/nagare/cache/torrent',
    cacheBytes: 0,
  },
}

function outcome(source: string, overrides: Partial<SourceOutcome> = {}): SourceOutcome {
  return { source, state: 'ok', count: 0, rawCount: 0, dropped: 0, latencyMs: 50, ...overrides }
}

async function mountAt(path: string) {
  const router = createAppRouter(createMemoryHistory({ initialEntries: [path] }))
  const mounted = await mount(<RouterProvider router={router} />)
  // 路由懒加载 + 首屏数据请求：再让一轮微任务落定
  await act(async () => {})
  return { router, ...mounted }
}

beforeEach(() => {
  vi.stubGlobal('scrollTo', vi.fn())
  installLocalStorage()
  vi.mocked(fetchSettings).mockResolvedValue(SETTINGS)
  vi.mocked(fetchTorrentStatus).mockResolvedValue({
    active: true,
    phase: 'metadata',
    peers: 3,
    seeders: 1,
    downRate: 1024,
    upRate: 0,
    buffered: 0,
    progress: 0,
    cacheBytes: 0,
    seeding: false,
  })
  vi.mocked(startTorrentPlay).mockReturnValue(new Promise<TorrentPlayData>(() => {}))
  vi.mocked(stopTorrent).mockResolvedValue(undefined)
  vi.mocked(fetchSources).mockResolvedValue(TWO_SOURCES)
  vi.mocked(fetchUpdate).mockResolvedValue({
    enabled: true,
    current: '0.1.0',
    latest: '',
    available: false,
    url: '',
    checkedAt: null,
    error: '',
    selfUpdate: { supported: true, channel: 'app-bundle', target: '/Applications/Nagare.app' },
  })
  vi.mocked(searchMagnets).mockResolvedValue({ query: '', items: [], sources: [] })
  vi.mocked(setSourceEnabled).mockResolvedValue(undefined)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('SearchPage（空态）', () => {
  it('未搜索：有规则、没 q → 引导输入关键词，不发搜索请求', async () => {
    const { container, unmount } = await mountAt('/search')
    expect(container.textContent).toContain('输入关键词开始搜索')
    expect(container.textContent).toContain('已加载 2 个源，2 个启用')
    expect(searchMagnets).not.toHaveBeenCalled()
    // 顶栏入口
    expect(container.querySelector('a[href="/"]')).not.toBeNull()
    expect(container.querySelector('a[href="/settings"]')).not.toBeNull()
    await unmount()
  })

  it('无规则：sources 为空 → 引导去设置页添加规则来源，并列出规则加载错误', async () => {
    vi.mocked(fetchSources).mockResolvedValue({
      sources: [],
      rules: { ...RULES, loaded: 0, errors: ['broken.yaml: 缺少 name 字段'] },
    })
    const { container, unmount } = await mountAt('/search?q=frieren')
    expect(container.textContent).toContain('还没有规则来源')
    expect(container.textContent).toContain('nagare 不内置任何磁力源')
    expect(container.querySelector('a.search-empty-link')?.getAttribute('href')).toBe('/settings')
    expect(container.querySelector('.rules-errors')?.textContent).toContain('broken.yaml: 缺少 name 字段')
    // 引导文案不得出现任何具体站点
    expect(container.textContent).not.toMatch(/https?:\/\//)
    await unmount()
  })

  it('搜了但为空：源状态条是主角 —— 规则改坏的源显示「源异常」而不是「无结果」', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({
      query: 'frieren',
      items: [],
      sources: [
        outcome('src-a', { state: 'zero' }),
        outcome('src-b', {
          state: 'dead',
          rawCount: 25,
          dropped: 25,
          reason: '规则解析不出任何条目',
          detail: 'selector "item title" matched 0 nodes',
        }),
      ],
    })
    const { container, unmount } = await mountAt('/search?q=frieren')

    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith('frieren')
    const chips = container.querySelectorAll('.src-bar .src-chip')
    expect(chips).toHaveLength(2)

    const zeroChip = container.querySelector('.src-chip--zero')
    expect(zeroChip?.textContent).toContain('源甲')
    expect(zeroChip?.textContent).toContain('无结果')

    const deadChip = container.querySelector('.src-chip--dead')
    expect(deadChip?.textContent).toContain('源乙')
    expect(deadChip?.textContent).toContain('源异常')
    expect(deadChip?.textContent).not.toContain('无结果')
    expect(deadChip?.getAttribute('title')).toContain('规则解析不出任何条目')
    expect(deadChip?.getAttribute('title')).toContain('selector "item title" matched 0 nodes')

    // 空结果文案指向源问题，而不是「没资源」
    expect(container.textContent).toContain('没有结果，且有源出了问题')
    expect(container.textContent).toContain('1 个源异常')
    expect(container.querySelector('.res-table')).toBeNull()
    await unmount()
  })
})

describe('SearchPage（结果与交互）', () => {
  const RESULT: SearchResult = {
    query: 'frieren',
    items: [
      {
        title: '[Sakurato] Sousou no Frieren [01][1080p]',
        magnet: 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567',
        size: '1.5 GB',
        fansub: '桜都字幕组',
        date: '2026-08-30',
        source: 'src-a',
        seeders: 12,
      },
      {
        title: '[Sakurato] Sousou no Frieren [02][1080p]',
        magnet: 'magnet:?xt=urn:btih:89abcdef0123456789abcdef0123456789abcdef',
        size: '',
        fansub: null,
        date: null,
        source: 'src-b',
      },
    ],
    sources: [outcome('src-a', { count: 1, rawCount: 1 }), outcome('src-b', { count: 1, rawCount: 1 })],
  }

  it('有结果：渲染结果表、来源徽标用规则名、状态行给出摘要；URL 里的 q 回填输入框', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    const { container, unmount } = await mountAt('/search?q=frieren')

    expect((container.querySelector('input[name="q"]') as HTMLInputElement).value).toBe('frieren')
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2)
    expect(container.querySelector('th.res-seeders')).not.toBeNull()
    const badges = Array.from(container.querySelectorAll('.res-source .badge')).map((el) => el.textContent)
    expect(badges).toEqual(['源甲', '源乙'])
    expect(container.querySelector('.search-status')?.textContent).toBe('2 条 · 2 个源正常')
    // 引擎正常时播放按钮全部可点（M3 起边下边播上线）
    const plays = Array.from(container.querySelectorAll('.res-play-wrap button')) as HTMLButtonElement[]
    expect(plays).toHaveLength(2)
    expect(plays.every((button) => button.disabled)).toBe(false)
    await unmount()
  })

  it('磁力引擎不可用：顶部警示 + 播放按钮全部禁用（降级也要看得见）', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    vi.mocked(fetchSettings).mockResolvedValue({
      ...SETTINGS,
      torrent: { ...SETTINGS.torrent, enabled: false },
    })
    const { container, unmount } = await mountAt('/search?q=frieren')

    expect(container.querySelector('.alert-warn')?.textContent).toContain('磁力引擎启动失败')
    const plays = Array.from(container.querySelectorAll('.res-play-wrap button')) as HTMLButtonElement[]
    expect(plays.every((button) => button.disabled)).toBe(true)
    await unmount()
  })

  /** 点第 index 行的「播放」 */
  async function clickPlay(container: HTMLElement, index: number): Promise<void> {
    const plays = Array.from(container.querySelectorAll('.res-play-wrap button')) as HTMLButtonElement[]
    await act(async () => {
      plays[index]?.click()
    })
    await act(async () => {})
  }

  it('点「播放」：起播中出现状态条，本行显示「启动中 …」，另一行被禁用', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    const { container, unmount } = await mountAt('/search?q=frieren')
    await clickPlay(container, 0)

    expect(startTorrentPlay).toHaveBeenCalledExactlyOnceWith(
      { magnet: RESULT.items[0]?.magnet, title: RESULT.items[0]?.title },
      expect.any(AbortSignal),
    )
    expect(container.querySelector('.tsb-bar')).not.toBeNull()
    expect(container.querySelector('.tsb-readout')?.textContent).toContain('分享者 3')

    const plays = Array.from(container.querySelectorAll('.res-play-wrap button')) as HTMLButtonElement[]
    expect(plays[0]?.textContent).toBe('启动中 …')
    expect(plays[0]?.disabled).toBe(true)
    // 同一时刻只允许一条磁力：另一行禁用并说明原因
    expect(plays[1]?.disabled).toBe(true)
    expect(
      container.querySelectorAll('.res-play-wrap')[1]?.getAttribute('title'),
    ).toContain('已有一条磁力在播放')

    await unmount()
  })

  it('点状态条的「取消」：调 stop 并把界面收回空闲', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    const { container, unmount } = await mountAt('/search?q=frieren')
    await clickPlay(container, 0)

    const cancel = Array.from(container.querySelectorAll('.tsb-actions button')).find(
      (button) => button.textContent === '取消',
    ) as HTMLButtonElement
    await act(async () => {
      cancel.click()
    })
    await act(async () => {})

    expect(stopTorrent).toHaveBeenCalledTimes(1)
    expect(container.querySelector('.tsb-bar')).toBeNull()
    const plays = Array.from(container.querySelectorAll('.res-play-wrap button')) as HTMLButtonElement[]
    expect(plays.every((button) => button.disabled)).toBe(false)

    await unmount()
  })

  it('needSelection：弹出选集弹窗，选定后带 fileIndex 重发', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    vi.mocked(startTorrentPlay)
      .mockResolvedValueOnce({
        needSelection: true,
        files: [
          { index: 0, name: 'ep01.mkv', path: 'ep01.mkv', sizeBytes: 1024, episode: 1 },
          { index: 4, name: 'ep02.mkv', path: 'ep02.mkv', sizeBytes: 1024, episode: 2 },
        ],
      })
      .mockReturnValueOnce(new Promise<TorrentPlayData>(() => {}))
    const { container, unmount } = await mountAt('/search?q=frieren')
    await clickPlay(container, 0)

    const dialog = container.querySelector('[role="dialog"]')
    expect(dialog).not.toBeNull()
    expect(container.querySelectorAll('.ep-item')).toHaveLength(2)

    await act(async () => {
      container.querySelectorAll<HTMLButtonElement>('.ep-item')[1]?.click()
    })
    await act(async () => {})

    expect(startTorrentPlay).toHaveBeenNthCalledWith(
      2,
      { magnet: RESULT.items[0]?.magnet, title: RESULT.items[0]?.title, fileIndex: 4 },
      expect.any(AbortSignal),
    )
    expect(container.querySelector('[role="dialog"]')).toBeNull()

    await unmount()
  })

  // 回归：选集弹窗关掉之后焦点必须回到那一行的播放按钮。
  // 弹窗自己读 document.activeElement 是读不到它的 —— 按钮点下的同一次渲染就自我
  // 禁用了，禁用元素留不住焦点，等弹窗挂载时 activeElement 已经是 <body>。
  it('放弃选集：焦点交还给那一行的播放按钮，而不是丢回 <body>', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    vi.mocked(startTorrentPlay).mockResolvedValue({
      needSelection: true,
      files: [{ index: 0, name: 'ep01.mkv', path: 'ep01.mkv', sizeBytes: 1024, episode: 1 }],
    })
    const { container, unmount } = await mountAt('/search?q=frieren')
    const trigger = container.querySelectorAll<HTMLButtonElement>('.res-play-wrap button')[0]
    await clickPlay(container, 0)
    expect(container.querySelector('[role="dialog"]')).not.toBeNull()

    const cancel = Array.from(container.querySelectorAll('.ep-actions button')).find(
      (button) => button.textContent === '取消',
    ) as HTMLButtonElement
    await act(async () => {
      cancel.click()
    })
    await act(async () => {
      await new Promise((resolve) => requestAnimationFrame(() => resolve(null)))
    })

    expect(container.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(trigger)

    await unmount()
  })

  // 停止失败时界面已经回到空闲（前端确实不再等了），但错误不能静默 ——
  // 后端可能还留着种子，用户需要知道并有下一步动作。
  it('停止失败：状态行给出中文原因 + 去设置页清缓存的下一步', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    vi.mocked(stopTorrent).mockRejectedValue(new Error('后端没有响应'))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const { container, unmount } = await mountAt('/search?q=frieren')
    await clickPlay(container, 0)

    const cancel = Array.from(container.querySelectorAll('.tsb-actions button')).find(
      (button) => button.textContent === '取消',
    ) as HTMLButtonElement
    await act(async () => {
      cancel.click()
    })
    await act(async () => {})

    const status = container.querySelector('.search-status')?.textContent ?? ''
    expect(status).toContain('后端没有响应')
    expect(status).toContain('清空磁力缓存')

    await unmount()
  })

  it('起播失败：状态条显示后端中文错误 + 重试；其他行恢复可点', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    vi.mocked(startTorrentPlay).mockRejectedValue(new Error('找不到可用的分享者，换一条资源试试'))
    const { container, unmount } = await mountAt('/search?q=frieren')
    await clickPlay(container, 0)

    expect(container.querySelector('.tsb-bar--error')).not.toBeNull()
    expect(container.querySelector('.tsb-phase[role="alert"]')?.textContent).toBe(
      '找不到可用的分享者，换一条资源试试',
    )
    const plays = Array.from(container.querySelectorAll('.res-play-wrap button')) as HTMLButtonElement[]
    expect(plays.every((button) => button.disabled)).toBe(false)

    await unmount()
  })

  it('点 chip 切换启用状态：调 enabled 接口后自动重搜', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    const { container, unmount } = await mountAt('/search?q=frieren')
    expect(searchMagnets).toHaveBeenCalledTimes(1)

    const chip = container.querySelector('.src-bar button.src-chip') as HTMLButtonElement
    expect(chip.getAttribute('aria-pressed')).toBe('true')
    await act(async () => {
      chip.click()
    })
    await act(async () => {})

    expect(setSourceEnabled).toHaveBeenCalledExactlyOnceWith('src-a', false)
    expect(fetchSources).toHaveBeenCalledTimes(2)
    expect(searchMagnets).toHaveBeenCalledTimes(2)
    expect(searchMagnets).toHaveBeenLastCalledWith('frieren')
    await unmount()
  })

  it('提交表单：关键词写进 URL ?q=，并触发搜索', async () => {
    const { container, router, unmount } = await mountAt('/search')
    const input = container.querySelector('input[name="q"]') as HTMLInputElement
    const form = container.querySelector('form[role="search"]') as HTMLFormElement

    await act(async () => {
      // React 受控输入：走原生 setter 再派发 input 事件
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      setter?.call(input, '  葬送的芙莉莲 ')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      form.requestSubmit()
    })
    await act(async () => {})

    expect(router.state.location.search).toEqual({ q: '葬送的芙莉莲' })
    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith('葬送的芙莉莲')
    await unmount()
  })
})
