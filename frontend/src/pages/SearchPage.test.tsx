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
import { accessibleName, getAllByRole, getByRole, queryByRole } from '../test/queries'
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
  background: { mode: 'dock' }, player: { anime4k: 'off' },
  torrent: {
    enabled: true,
    seeding: false,
    trackers: [],
    useDefaultTrackers: true,
    defaultTrackers: [],
    portForwarding: true,
    listenPort: 6881,
    cacheDir: '/tmp/nagare/cache/torrent',
    cacheBytes: 0,
    downloadDir: '/Users/you/Downloads/nagare',
    downloadDirIsDefault: true,
    restartRequired: false,
  },
}

function outcome(source: string, overrides: Partial<SourceOutcome> = {}): SourceOutcome {
  return { source, state: 'ok', count: 0, rawCount: 0, dropped: 0, latencyMs: 50, ...overrides }
}

/** 每一行的「播放」按钮：名字 = 看得见的字 + 这一行的标题 */
function playButtons(container: HTMLElement): HTMLButtonElement[] {
  const table = getByRole(container, 'table')
  return getAllByRole(table, 'button', { name: /^(播放|启动中 …|播放中) / }) as HTMLButtonElement[]
}

/** aria-describedby 指向的说明文字（播放按钮禁用的原因） */
function description(element: HTMLElement): string {
  const ids = element.getAttribute('aria-describedby')?.split(/\s+/) ?? []
  return ids.map((id) => element.ownerDocument.getElementById(id)?.textContent ?? '').join(' ')
}

/** 源状态条里的每个 chip（可切换启用的按钮） */
function sourceChips(container: HTMLElement): HTMLButtonElement[] {
  return getAllByRole(getByRole(container, 'list', { name: '各源状态' }), 'button') as HTMLButtonElement[]
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
    expect(getByRole(container, 'heading', { level: 2, name: '输入关键词开始搜索' })).not.toBeNull()
    expect(container.textContent).toContain('已加载 2 个源，2 个启用')
    expect(searchMagnets).not.toHaveBeenCalled()
    // 顶栏入口
    const banner = getByRole(container, 'banner')
    expect(getByRole(banner, 'link', { name: 'nagare（发现）' }).getAttribute('href')).toBe('/')
    expect(getByRole(banner, 'link', { name: '设置' }).getAttribute('href')).toBe('/settings')
    await unmount()
  })

  it('无规则：sources 为空 → 引导去设置页添加规则来源，并列出规则加载错误', async () => {
    vi.mocked(fetchSources).mockResolvedValue({
      sources: [],
      rules: { ...RULES, loaded: 0, errors: ['broken.yaml: 缺少 name 字段'] },
    })
    const { container, unmount } = await mountAt('/search?q=frieren')
    expect(getByRole(container, 'heading', { level: 2, name: '还没有规则来源' })).not.toBeNull()
    expect(container.textContent).toContain('nagare 不内置任何磁力源')
    expect(getByRole(container, 'link', { name: '去设置添加规则来源' }).getAttribute('href')).toBe('/settings#sources')
    expect(getByRole(container, 'alert', { name: /broken\.yaml: 缺少 name 字段/ })).not.toBeNull()
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
    expect(sourceChips(container).map(accessibleName)).toEqual(['源甲无结果，启用此源', '源乙源异常，启用此源'])

    // 改坏的规则说「源异常」，不说「无结果」；原因在悬停提示里
    const deadChip = sourceChips(container)[1]!
    expect(deadChip.getAttribute('title')).toContain('规则解析不出任何条目')
    expect(deadChip.getAttribute('title')).toContain('selector "item title" matched 0 nodes')

    // 空结果文案指向源问题，而不是「没资源」
    expect(getByRole(container, 'heading', { level: 2, name: '没有结果，且有源出了问题' })).not.toBeNull()
    expect(container.textContent).toContain('1 个源异常')
    expect(queryByRole(container, 'table')).toBeNull()
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

    expect((getByRole(container, 'textbox', { name: '搜索关键词' }) as HTMLInputElement).value).toBe('frieren')
    const table = getByRole(container, 'table')
    // 一行表头 + 两行结果
    expect(getAllByRole(table, 'row')).toHaveLength(3)
    expect(getAllByRole(table, 'columnheader').map(accessibleName)).toContain('做种')
    expect(getAllByRole(table, 'cell', { name: /^源[甲乙]$/ }).map(accessibleName)).toEqual(['源甲', '源乙'])
    expect(getByRole(container, 'status', { name: '2 条 · 2 个源正常' })).not.toBeNull()
    // 引擎正常时播放按钮全部可点（M3 起边下边播上线）
    const plays = playButtons(container)
    expect(plays).toHaveLength(2)
    expect(plays.every((button) => button.disabled)).toBe(false)
    await unmount()
  })

  it('磁力引擎不可用：顶部警示 + 播放按钮全部禁用（降级也要看得见）', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    vi.mocked(fetchSettings).mockResolvedValue({
      ...SETTINGS,
      torrent: { ...SETTINGS.torrent!, enabled: false },
    })
    const { container, unmount } = await mountAt('/search?q=frieren')

    expect(getByRole(container, 'alert', { name: /磁力引擎启动失败/ })).not.toBeNull()
    expect(playButtons(container).every((button) => button.disabled)).toBe(true)
    await unmount()
  })

  /** 点第 index 行的「播放」 */
  async function clickPlay(container: HTMLElement, index: number): Promise<void> {
    const plays = playButtons(container)
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
    const bar = getByRole(container, 'region', { name: '磁力缓冲进度' })
    expect(bar.textContent).toContain('分享者 3')

    const plays = playButtons(container)
    expect(plays[0]?.textContent).toBe('启动中 …')
    // 本行在起播：不可用但不是 disabled（焦点留在它身上）
    expect(plays[0]?.getAttribute('aria-disabled')).toBe('true')
    expect(plays[0]?.disabled).toBe(false)
    // 同一时刻只允许一条磁力：另一行禁用并说明原因
    expect(plays[1]?.disabled).toBe(true)
    expect(description(plays[1]!)).toContain('已有一条磁力在播放')

    await unmount()
  })

  it('点状态条的「取消」：调 stop 并把界面收回空闲', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    const { container, unmount } = await mountAt('/search?q=frieren')
    await clickPlay(container, 0)

    const cancel = getByRole(getByRole(container, 'region', { name: '磁力缓冲进度' }), 'button', { name: '取消' })
    await act(async () => {
      cancel.click()
    })
    await act(async () => {})

    expect(stopTorrent).toHaveBeenCalledTimes(1)
    expect(queryByRole(container, 'region', { name: '磁力缓冲进度' })).toBeNull()
    expect(playButtons(container).every((button) => button.disabled)).toBe(false)

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

    const dialog = getByRole(container, 'dialog', { name: '选择要播放的剧集' })
    const files = getAllByRole(dialog, 'button', { name: /^播放 / })
    expect(files.map(accessibleName)).toEqual(['播放 01ep01.mkv1 KB', '播放 02ep02.mkv1 KB'])

    await act(async () => {
      files[1]?.click()
    })
    await act(async () => {})

    expect(startTorrentPlay).toHaveBeenNthCalledWith(
      2,
      { magnet: RESULT.items[0]?.magnet, title: RESULT.items[0]?.title, fileIndex: 4 },
      expect.any(AbortSignal),
    )
    expect(queryByRole(container, 'dialog')).toBeNull()

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
    const trigger = playButtons(container)[0]
    await clickPlay(container, 0)
    const dialog = getByRole(container, 'dialog', { name: '选择要播放的剧集' })

    const cancel = getByRole(dialog, 'button', { name: '取消' })
    await act(async () => {
      cancel.click()
    })
    await act(async () => {
      await new Promise((resolve) => requestAnimationFrame(() => resolve(null)))
    })

    expect(queryByRole(container, 'dialog')).toBeNull()
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

    const cancel = getByRole(getByRole(container, 'region', { name: '磁力缓冲进度' }), 'button', { name: '取消' })
    await act(async () => {
      cancel.click()
    })
    await act(async () => {})

    const status = getByRole(container, 'alert', { name: /后端没有响应/ })
    expect(status.textContent).toContain('清空磁力缓存')

    await unmount()
  })

  it('起播失败：状态条显示后端中文错误 + 重试；其他行恢复可点', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    vi.mocked(startTorrentPlay).mockRejectedValue(new Error('找不到可用的分享者，换一条资源试试'))
    const { container, unmount } = await mountAt('/search?q=frieren')
    await clickPlay(container, 0)

    const bar = getByRole(container, 'region', { name: '磁力播放失败' })
    expect(getByRole(bar, 'alert').textContent).toBe('找不到可用的分享者，换一条资源试试')
    expect(getByRole(bar, 'button', { name: '重试' })).not.toBeNull()
    expect(playButtons(container).every((button) => button.disabled)).toBe(false)

    await unmount()
  })

  it('点 chip 切换启用状态：调 enabled 接口后自动重搜', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    const { container, unmount } = await mountAt('/search?q=frieren')
    expect(searchMagnets).toHaveBeenCalledTimes(1)

    const chip = sourceChips(container)[0]!
    expect(accessibleName(chip)).toBe('源甲1 条，正常，启用此源')
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
    const input = getByRole(container, 'textbox', { name: '搜索关键词' }) as HTMLInputElement
    const form = getByRole(container, 'search', { name: '磁力搜索' }) as HTMLFormElement

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
