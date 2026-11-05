// @vitest-environment jsdom
// 整页冒烟：真实路由 + 真实 apiFetch，只在 fetch 边界打桩。
// 证明「路由 → 页面 → hooks → 组件」整条装配线能对着契约数据渲染出界面。
import { RouterProvider } from '@tanstack/react-router'
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { LibraryData, PlayerStatus, SettingsData, UpdateView } from '../lib/endpoints'
import { TOKEN_STORAGE_KEY } from '../lib/token'
import { router } from '../routes'
import { mount } from '../test/harness'
import { accessibleName, getAllByRole, getAllByText, getByRole, getByText, queryByRole, queryByText } from '../test/queries'
import { installLocalStorage } from '../test/storage'
import { SKIP_FOLDERS_KEY } from '../components/library/onboardingSkip'
import { AUTO_RESCAN_AFTER_MS, resetAutoRescanForTest } from '../hooks/useAutoRescan'

const LIBRARY: LibraryData = {
  continueWatching: [],
  folders: [{ id: 'folder-1', path: '/Users/you/Movies/Anime', addedAt: 1_756_500_000 }],
  clusters: [
    {
      clusterKey: 'frieren',
      title: '葬送的芙莉莲',
      season: 1,
      confidence: 0.93,
      episodeCount: 2,
      groups: [
        {
          groupKey: 'main',
          label: '正片',
          sortMode: 'episode',
          items: [
            {
              fileId: 'file-1',
              fileName: '[Sakurato] Sousou no Frieren [01][1080p].mkv',
              episode: 1,
              kind: 'main',
              resolution: '1080p',
              sizeBytes: 1_610_612_736,
              progress: { positionSec: 710, durationSec: 1420, completed: false },
            },
            {
              fileId: 'file-2',
              fileName: '[Sakurato] Sousou no Frieren [02][1080p].mkv',
              episode: 2,
              kind: 'main',
              resolution: '1080p',
              sizeBytes: 1_610_612_736,
              progress: null,
            },
          ],
        },
      ],
    },
  ],
  // 刚扫过：打开页面不触发自动重扫（自动重扫另有专门的用例）
  scannedAt: Date.now(),
}

const SETTINGS: SettingsData = {
  version: '0.1.0',
  platform: 'darwin',
  arch: 'arm64',
  dataDir: '/Users/you/Library/Application Support/nagare',
  logPath: '/Users/you/Library/Application Support/nagare/nagare.log',
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
    cacheDir: '/Users/you/Library/Application Support/nagare/cache/torrent',
    cacheBytes: 0,
    downloadDir: '/Users/you/Downloads/nagare',
    downloadDirIsDefault: true,
    restartRequired: false,
  },
}

const MPV_MISSING: SettingsData = {
  ...SETTINGS,
  mpv: {
    found: false,
    hint: '未检测到 mpv，macOS 需要自行安装。',
    install: { command: 'brew install mpv', url: 'https://mpv.io/installation/', note: '' },
  },
}

/** 桌面窗口（进程内 libmpv）：命令行 mpv 有没有都无所谓，后端恒报已就绪 + embedded */
const MPV_EMBEDDED: SettingsData = { ...SETTINGS, mpv: { found: true, embedded: true, source: 'embedded' } }
const MPV_BUNDLED: SettingsData = {
  ...SETTINGS,
  mpv: { found: true, path: '/Applications/Nagare.app/Contents/MacOS/mpv', version: '0.41.0', source: 'bundled' },
}

const PLAYER: PlayerStatus = { playing: false }

const UPDATE: UpdateView = {
  enabled: true,
  current: '0.1.0',
  latest: '',
  available: false,
  url: '',
  checkedAt: null,
  error: '',
  selfUpdate: { supported: true, channel: 'app-bundle', target: '/Applications/Nagare.app' },
}

/** 按路径分发的 fetch 桩，一律返回统一信封 */
function stubFetch(
  settings: SettingsData = SETTINGS,
  library: LibraryData = LIBRARY,
  player: PlayerStatus = PLAYER,
): ReturnType<typeof vi.fn> {
  const impl = async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const path = new URL(url, 'http://127.0.0.1').pathname
    const payload: Record<string, unknown> = {
      '/api/library': library,
      '/api/settings': settings,
      '/api/player/status': player,
      '/api/update': UPDATE,
    }
    const data = payload[path]
    if (data === undefined) {
      return new Response(JSON.stringify({ success: false, data: null, error: `未打桩的路径 ${path}` }), {
        status: 404,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    return new Response(JSON.stringify({ success: true, data }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  const mock = vi.fn(impl)
  vi.stubGlobal('fetch', mock)
  return mock
}

beforeEach(() => {
  resetAutoRescanForTest()
  // apiFetch 会读 sessionStorage 里的 token（形状必须合法）
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '9f86d081884c7d659a2feaa0c55ad015')
  // 媒体库在 /library（第一屏 / 是发现页）
  window.history.replaceState(null, '', '/library')
  // jsdom 没实现 scrollTo，TanStack Router 挂载时会调一次；打桩消掉告警噪音
  vi.stubGlobal('scrollTo', vi.fn())
  // 根布局的 UpdateBanner 会读 localStorage（Node 自带的全局壳在测试里不可用）
  installLocalStorage()
})

afterEach(() => {
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

/** 作品网格（列表名「媒体库」）里的每张卡：链接的名字就是卡片上的可见文字，作品名打头 */
const works = (container: HTMLElement) => {
  const grid = queryByRole(container, 'list', { name: '媒体库' })
  return grid === null ? [] : (getAllByRole(grid, 'link') as HTMLAnchorElement[])
}
/** 「还没认出的 N 组」：默认折叠的那一节（标题就是它的 summary） */
const unrecognized = (container: HTMLElement) => queryByText(container, /^还没认出的 \d+ 组$/)?.closest('details') ?? null
/** 那一节里的分组链接：收起时读屏碰不到，照样数得到 */
const unrecognizedLinks = (container: HTMLElement) => getAllByRole(unrecognized(container)!, 'link', { hidden: true })
const filterButton = (container: HTMLElement, text: string) => getByRole(getByRole(container, 'group', { name: '按观看状态筛选' }), 'button', { name: text }) as HTMLButtonElement
/** 页面上各条状态（role=status）的文字 */
const statuses = (container: HTMLElement) => getAllByRole(container, 'status').map((el) => el.textContent ?? '')

describe('LibraryPage（整页冒烟）', () => {
  it('低置信又没认过的作品标「待确认」；认过作品就不标，并改用作品名、两种名字都能搜到', async () => {
    const library = structuredClone(LIBRARY)
    const unsure = { ...structuredClone(library.clusters[0]!), clusterKey: 'unsure', title: 'Unsure Show', confidence: 0.4 }
    const chosen = { ...structuredClone(library.clusters[0]!), clusterKey: 'chosen', title: 'Chosen Folder', confidence: 0.4, association: { mode: 'manual' as const, anilistId: 7, title: '认定的作品', setAt: 1 } }
    library.clusters = [unsure, chosen]
    stubFetch(SETTINGS, library)
    const { container, unmount } = await mount(<RouterProvider router={router} />)

    // 认过的进作品网格，不标「待确认」
    expect(works(container)).toHaveLength(1)
    expect(accessibleName(works(container)[0]!)).toMatch(/^认定的作品/)
    expect(works(container)[0]!.textContent).not.toContain('待确认')
    // 没认出的收进末尾默认折叠的一节，标「待确认」，主操作是认作品
    const rest = unrecognized(container)!
    expect(rest.open).toBe(false)
    const [row] = unrecognizedLinks(container)
    expect(row!.textContent).toContain('Unsure Show')
    expect(row!.textContent).toContain('待确认')
    expect(row!.textContent).toContain('认作品')
    expect(row!.getAttribute('href')).toBe('/anime/unsure')

    const search = getByRole(container, 'textbox', { name: '筛选媒体库' }) as HTMLInputElement
    for (const q of ['Chosen Folder', '认定的']) {
      await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search, q)
        search.dispatchEvent(new Event('input', { bubbles: true }))
      })
      expect(works(container).map((a) => accessibleName(a).startsWith('认定的作品'))).toEqual([true])
      expect(unrecognized(container)).toBeNull()
    }
    await unmount()
  })

  it('「还没认出」用户展开过：筛选把这一节整个藏掉、再清掉筛选回来时，仍是展开的', async () => {
    const library = structuredClone(LIBRARY)
    const unsure = { ...structuredClone(library.clusters[0]!), clusterKey: 'unsure', title: 'Unsure Show' }
    const chosen = { ...structuredClone(library.clusters[0]!), clusterKey: 'chosen', title: 'Chosen Folder', association: { mode: 'manual' as const, anilistId: 7, title: '认定的作品', setAt: 1 } }
    library.clusters = [unsure, chosen]
    stubFetch(SETTINGS, library)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const search = getByRole(container, 'textbox', { name: '筛选媒体库' }) as HTMLInputElement
    const type = async (q: string) => act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search, q)
      search.dispatchEvent(new Event('input', { bubbles: true }))
    })

    // 网格里有作品：默认收起；用户自己展开
    expect(unrecognized(container)?.open).toBe(false)
    await act(async () => {
      const details = unrecognized(container)!
      details.open = true
      details.dispatchEvent(new Event('toggle'))
    })
    expect(unrecognized(container)?.open).toBe(true)

    await type('认定的')
    expect(unrecognized(container)).toBeNull()
    await type('')
    expect(unrecognized(container)?.open).toBe(true)

    // 收起也一样记得：网格空了才替用户展开，网格又有内容时不替他收起
    await act(async () => {
      const details = unrecognized(container)!
      details.open = false
      details.dispatchEvent(new Event('toggle'))
    })
    await type('认定的')
    await type('')
    expect(unrecognized(container)?.open).toBe(false)
    await type('Unsure')
    expect(works(container)).toHaveLength(0)
    expect(unrecognized(container)?.open).toBe(true)
    await type('')
    expect(unrecognized(container)?.open).toBe(true)
    await unmount()
  })

  it('自动认出的同一部作品的几个版本并成一张海报，链到作品详情页；识别进行中状态行说出进度', async () => {
    const library = structuredClone(LIBRARY)
    const base = library.clusters[0]!
    const ani = { ...structuredClone(base), clusterKey: 'ani', title: 'Re：從零開始的異世界生活 第四季', matched: { anilistId: 189046, title: 'Re：从零开始的异世界生活 第四季' }, cover: '/art/cap/ani' }
    const loli = { ...structuredClone(base), clusterKey: 'loli', title: 'Re Zero kara Hajimeru Isekai Seikatsu', matched: { anilistId: 189046, title: 'Re：从零开始的异世界生活 第四季' } }
    for (const item of loli.groups[0]!.items) item.fileId = `loli-${item.fileId}`
    library.clusters = [ani, loli]
    library.identify = { running: true, done: 1, total: 2 }
    stubFetch(SETTINGS, library)
    const { container, unmount } = await mount(<RouterProvider router={router} />)

    const posters = works(container)
    expect(posters).toHaveLength(1)
    expect(accessibleName(posters[0]!)).toMatch(/^Re：从零开始的异世界生活 第四季/)
    expect(posters[0]!.textContent).toContain('2 个版本')
    expect(posters[0]!.querySelector('img')?.getAttribute('src')).toBe('/art/cap/ani')
    expect(posters[0]!.getAttribute('href')).toBe('/entry?id=189046')
    expect(getByRole(container, 'heading', { level: 2, name: '作品 1' })).toBeDefined()
    // 识别进度单独一行，不在 live region 里（每几秒变一次，读屏不该一条条念）
    const identify = getByText(container, /^正在识别作品 1\/2/)
    expect(identify.closest('[aria-live], [role="status"]')).toBeNull()
    await unmount()
  })

  it('后台还在认作品：没认出的先以灰色占位留在网格里，说「认作品中…」，不收进折叠的那一节', async () => {
    const library = { ...structuredClone(LIBRARY), identify: { running: true, done: 0, total: 1 } }
    stubFetch(SETTINGS, library)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(works(container)).toHaveLength(1)
    expect(accessibleName(works(container)[0]!)).toContain('认作品中…')
    expect(works(container)[0]!.querySelector('img')).toBeNull()
    expect(unrecognized(container)).toBeNull()
    await unmount()
  })

  it('海报色写在每张作品卡自己的根元素上（悬停描边用）；没有色阶的卡不写', async () => {
    const library = structuredClone(LIBRARY)
    const base = library.clusters[0]!
    const toned = { ...structuredClone(base), clusterKey: 'toned', cover: '/art/cap/toned', matched: { anilistId: 1, title: '有色阶的作品' }, posterTones: { hue: 30, tone: '#e0b49a', quiet: '#c29a82', mid: '#8a6450', low: '#2b1d15', onTone: '#120a06' } }
    const plain = { ...structuredClone(base), clusterKey: 'plain', cover: '/art/cap/plain', matched: { anilistId: 2, title: '灰封面的作品' }, posterTones: null }
    for (const item of plain.groups[0]!.items) item.fileId = `plain-${item.fileId}`
    library.clusters = [toned, plain]
    stubFetch(SETTINGS, library)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const [first, second] = works(container)
    expect(first!.closest('li')!.style.getPropertyValue('--poster-tone-mid')).toBe('#8a6450')
    expect(second!.closest('li')!.getAttribute('style')).toBeNull()
    await unmount()
  })

  it('扫描读不到的文件夹：横幅里写出路径与原因，并指向设置里的文件夹管理', async () => {
    const library = structuredClone(LIBRARY)
    library.folders = [...library.folders, { id: 'folder-2', path: '/Volumes/外接盘/番剧', addedAt: 1, error: '目录不存在（移动硬盘没接上？）' }]
    stubFetch(SETTINGS, library)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const banner = getByText(container, /^有 1 个文件夹这次没能扫描/).closest('[role="status"]')!
    expect(getByText(banner as HTMLElement, '/Volumes/外接盘/番剧')).toBeDefined()
    expect(banner.textContent).toContain('目录不存在（移动硬盘没接上？）')
    expect(getByRole(banner as HTMLElement, 'link', { name: /检查文件夹/ }).getAttribute('href')).toBe('/settings#folders')
    await unmount()
  })

  it('所有文件夹都扫描正常时不出这条横幅', async () => {
    stubFetch()
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(container.textContent).not.toContain('没能扫描')
    await unmount()
  })

  it('距上次扫描已久时，打开媒体库页会静默重扫一次', async () => {
    const library = { ...structuredClone(LIBRARY), scannedAt: Date.now() - AUTO_RESCAN_AFTER_MS - 1000 }
    const fetchMock = stubFetch(SETTINGS, library)
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      const path = new URL(url, 'http://127.0.0.1').pathname
      const data = path === '/api/library/rescan' ? { stats: { videos: 2, clusters: 1 } } : path === '/api/library' ? library : path === '/api/settings' ? SETTINGS : path === '/api/update' ? UPDATE : PLAYER
      void init
      return new Response(JSON.stringify({ success: true, data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    })
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const posts = fetchMock.mock.calls.filter(([input, init]) => String(input).includes('/api/library/rescan') && (init as RequestInit | undefined)?.method === 'POST')
    expect(posts).toHaveLength(1)
    expect(statuses(container).some((text) => text.includes('失败'))).toBe(false)
    await unmount()
  })

  it('看完一集、下一集尚未开始的作品仍归入在看', async () => {
    const library = structuredClone(LIBRARY)
    library.clusters[0]!.groups[0]!.items[0]!.progress = { positionSec: 1420, durationSec: 1420, completed: true }
    stubFetch(SETTINGS, library)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    await act(async () => { filterButton(container, '在看').click() })
    expect(filterButton(container, '在看').getAttribute('aria-pressed')).toBe('true')
    // 还没认出作品：在「还没认出」那一节里；网格是空的，所以那一节自己展开
    expect(unrecognized(container)?.open).toBe(true)
    expect(getAllByRole(unrecognized(container)!, 'link')[0]?.textContent).toContain('葬送的芙莉莲')
    await act(async () => { filterButton(container, '已看完').click() })
    expect(works(container)).toHaveLength(0)
    expect(unrecognized(container)).toBeNull()
    expect(getByText(container, /^没有符合条件的作品/).getAttribute('role')).toBe('status')
    await unmount()
  })

  it('挂载后渲染顶栏、簇卡片、剧集行与上次扫描时间', async () => {
    const fetchMock = stubFetch()
    const { container, unmount } = await mount(<RouterProvider router={router} />)

    // 四份数据都请求过（library / settings / player status / 根布局的 update）
    const requested = fetchMock.mock.calls.map((call) => {
      const input = call[0] as RequestInfo | URL
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      return new URL(url, 'http://127.0.0.1').pathname
    })
    expect(requested).toEqual(
      expect.arrayContaining(['/api/library', '/api/settings', '/api/player/status', '/api/update']),
    )
    // 没有新版本：不出提示条
    expect(document.body.textContent).not.toContain('新版本')

    // 页头与 mpv 状态：点的颜色之外，状态以文字念得到
    expect(getByRole(container, 'heading', { level: 1 }).textContent).toContain('媒体库')
    expect(getByText(container, 'mpv 已就绪 · 0.38.0').getAttribute('title')).toBe('mpv 已就绪 · 0.38.0')
    expect(getAllByRole(container, 'alert')).toHaveLength(0)

    // 还没认出作品的分组：标题 + 季 + 集数；置信度 0.93 不出「待确认」。网格里一部都没有时这一节自己展开
    const rest = unrecognized(container)!
    expect(rest.open).toBe(true)
    const [row] = getAllByRole(rest, 'link')
    expect(row!.textContent).toContain('葬送的芙莉莲')
    expect(row!.textContent).toContain('第 1 季 · 2 集')
    expect(container.textContent).not.toContain('待确认')

    // 剧集列表【不】在库页上：它住在 /anime/$clusterKey，这一行链过去
    expect(getAllByRole(container, 'button', { name: /^播放/ })).toHaveLength(0)
    expect(row!.getAttribute('href')).toBe('/anime/frieren')

    // 未在播放：没有播放条（也就没有「停止」）
    expect(queryByRole(container, 'button', { name: '停止' })).toBeNull()

    await unmount()
  })

  it('mpv 未找到：状态点变红，提示条里给出「去设置安装 mpv」链接', async () => {
    stubFetch(MPV_MISSING)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(getByText(container, 'mpv 未找到')).toBeDefined()
    const alert = getByRole(container, 'alert')
    expect(alert.textContent).toContain('未检测到 mpv')
    const link = getByRole(alert, 'link', { name: /去设置安装 mpv/ })
    expect(link.getAttribute('href')).toBe('/settings#player')
    await unmount()
  })

  it('桌面窗口里 mpv 是内置的：状态点是绿的、说明是内置，不出「未检测到 mpv」的提示条', async () => {
    stubFetch(MPV_EMBEDDED)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const chip = getByText(container, /^mpv 已就绪/)
    expect(chip.textContent).toContain('内置')
    expect(chip.getAttribute('title')).toContain('内置')
    expect(queryByText(container, 'mpv 未找到')).toBeNull()
    expect(container.textContent).not.toContain('未检测到 mpv')
    expect(container.textContent).not.toContain('去设置安装 mpv')
    await unmount()
  })

  /** 库为空 + 未配规则仓库：最"空"的首次运行状态 */
  function stubFirstRun(
    settings: SettingsData = SETTINGS,
    detected: SettingsData['mpv'] = settings.mpv,
    pluginReady = false,
  ): ReturnType<typeof vi.fn> {
    // scannedAt 有值：删光文件夹时后端会重扫一次，空库也带着扫描时间（「上次扫描」要藏得住）
    const empty: LibraryData = { folders: [], clusters: [], continueWatching: [], scannedAt: 1_756_500_000 }
    let current = settings
    // 目录浏览的桩：常用位置 → 主目录 → 一个番剧文件夹
    const listings: Record<string, unknown> = {
      '': { path: '', parent: '', dirs: [{ name: '主目录', path: '/Users/you' }], truncated: false },
      '/Users/you': { path: '/Users/you', parent: '/Users', dirs: [{ name: 'Anime', path: '/Users/you/Anime' }], truncated: false },
      '/Users/you/Anime': { path: '/Users/you/Anime', parent: '/Users/you', dirs: [], truncated: false },
    }
    const noSources = {
      sources: [],
      rules: {
        remoteUrl: '',
        localDir: '',
        dir: '',
        loaded: 0,
        errors: [],
        lastLoadedAt: null,
        lastSyncAt: null,
      },
    }
    const impl = async (input: RequestInfo | URL): Promise<Response> => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      const parsed = new URL(url, 'http://127.0.0.1')
      const path = parsed.pathname
      if (path === '/api/mpv/detect') current = { ...current, mpv: detected }
      const data =
        path === '/api/library'
          ? empty
          : path === '/api/fs/dirs'
            ? listings[parsed.searchParams.get('path') ?? '']
          : path === '/api/library/folders'
            ? { folder: { id: 'f-1', path: '/Users/you/Anime', addedAt: 1 }, stats: { videos: 0, clusters: 0 } }
          : path === '/api/mpv/detect'
            ? detected
          : path === '/api/settings'
            ? current
            : path === '/api/sources'
              ? noSources
              : path === '/api/source-plugin'
                ? pluginReady
                  ? { config: { enabled: true, executable: '/x', root: '/y' }, status: { phase: 'ready' }, sources: [{ id: 'garden', name: 'Anime Garden', kind: 'bt', enabled: true }] }
                  : { config: { enabled: false, executable: '', root: '' }, status: { phase: 'disabled' }, sources: [] }
                : path === '/api/update'
                  ? UPDATE
                  : PLAYER
      return new Response(JSON.stringify({ success: true, data }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    const mock = vi.fn(impl)
    vi.stubGlobal('fetch', mock)
    return mock
  }

  /** 让在途的 fetch 与随后的 setState 都落地 */
  async function settle(): Promise<void> {
    for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve() })
  }

  function calledPaths(mock: ReturnType<typeof vi.fn>): string[] {
    return mock.mock.calls.map(([input]) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : (input as Request).url
      return new URL(url, 'http://127.0.0.1').pathname
    })
  }

  /** 引导屏：读屏里是一个叫「欢迎使用 nagare」的区块 */
  function onboarding(container: HTMLElement): HTMLElement | null {
    return queryByRole(container, 'region', { name: '欢迎使用 nagare' })
  }

  /** 引导屏里的一项：按小标题找到它所在的那一条 */
  function step(container: HTMLElement, name: string): HTMLElement {
    const item = getByRole(container, 'heading', { level: 3, name }).closest('li')
    if (item === null) throw new Error(`准备项「${name}」不在列表项里`)
    return item
  }

  /** 状态点只有颜色：读屏念的是标题后面那句隐藏的「已就绪」 */
  function isReady(item: HTMLElement): boolean {
    return queryByText(item, '，已就绪') !== null
  }

  function button(container: HTMLElement, name: string | RegExp): HTMLButtonElement {
    return getByRole(container, 'button', { name }) as HTMLButtonElement
  }

  it('桌面窗口的引导屏：mpv 一项直接是已就绪 · 内置，不给安装命令', async () => {
    stubFirstRun(MPV_EMBEDDED)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const mpv = step(container, '播放器 mpv')
    expect(isReady(mpv)).toBe(true)
    expect(mpv.textContent).toContain('内置')
    expect(mpv.textContent).not.toContain('brew install mpv')
    expect(getAllByRole(mpv, 'button')).toEqual([])
    await unmount()
  })

  it('浏览器模式用 Nagare.app 内置的 mpv：引导屏说明是随 nagare 附带的', async () => {
    stubFirstRun(MPV_BUNDLED)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const mpv = step(container, '播放器 mpv')
    expect(isReady(mpv)).toBe(true)
    expect(getByText(mpv, '已就绪 · 随 nagare 附带 · 0.41.0')).toBeDefined()
    expect(mpv.textContent).not.toContain('brew install mpv')
    await unmount()
  })

  it('库为空时显示引导屏，四个准备项齐全', async () => {
    stubFirstRun()

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const guide = onboarding(container)
    expect(guide).not.toBeNull()
    // 四项：mpv / 本地文件夹 / 磁力搜索 / 账号
    expect(getAllByRole(guide!, 'heading', { level: 3 }).map(accessibleName)).toEqual(['播放器 mpv', '本地文件夹', '磁力搜索', '账号'])
    // 添加文件夹的表单仍然内联在这里，不必点进另一个页面
    expect(getByRole(step(container, '本地文件夹'), 'textbox', { name: '文件夹绝对路径' })).toBeDefined()
    // mpv 已装 → 已就绪；其余三项都是待办
    expect(isReady(step(container, '播放器 mpv'))).toBe(true)
    expect(getAllByText(guide!, '，已就绪')).toHaveLength(1)
    await unmount()
  })

  /**
   * 端到端：树内软链子目录被跳过 → 用户在界面上读到原因【与恢复动作】。
   *
   * 这条守的是整条链路（ScanDir → ViewFolder.dropped → 库页），
   * 任何一环把丢弃吞掉都会红。丢弃在这个项目里的默认失败模式就是「静默」，
   * 所以断的是「看得见」，不是「记下来了」。
   */
  it('扫描跳过的东西要能在界面上读到原因与恢复动作', async () => {
    const withDrops: LibraryData = {
      ...LIBRARY,
      folders: [
        {
          ...LIBRARY.folders[0]!,
          dropped: {
            total: 3,
            groups: [
              {
                reason: 'symlink',
                count: 2,
                message: '符号链接，nagare 不跟随',
                recovery: '把链接指向的真实路径直接添加进库，或把软链换成硬链接',
                samples: ['/Users/you/Movies/Anime/Season2'],
              },
              {
                reason: 'too-small',
                count: 1,
                message: '视频文件小于 1MB',
                recovery: '多半是没下完的片或采样文件；下完之后重新扫描',
                samples: ['/Users/you/Movies/Anime/sample.mkv'],
              },
            ],
          },
        },
      ],
    }
    stubFetch(SETTINGS, withDrops)
    const { container, unmount } = await mount(<RouterProvider router={router} />)

    const count = queryByText(container, '3 项没有进库')
    expect(count, '扫描跳过了东西，界面上却没有任何痕迹').not.toBeNull()
    // 收起状态下也要看得见原因 —— 只说「3 项没有进库」等于什么都没说（原因写在 summary 里）
    const summary = count!.closest('summary')!
    expect(summary.textContent).toContain('符号链接')

    // 恢复动作：这才是这块东西存在的理由
    const recoveries = getAllByRole(summary.closest('details')!, 'listitem', { hidden: true }).map((li) => li.textContent ?? '').filter((text) => text.includes('→'))
    expect(recoveries[0]).toContain('真实路径')
    expect(recoveries).toHaveLength(2)
    // 具体是哪个文件，用户得能照着去找
    expect(container.textContent).toContain('/Users/you/Movies/Anime/Season2')
    await unmount()
  })

  it('一部作品都没扫到时，空态直接把原因摊开而不是让用户猜', async () => {
    const allDropped: LibraryData = {
      ...LIBRARY,
      clusters: [],
      folders: [
        {
          ...LIBRARY.folders[0]!,
          dropped: {
            total: 3,
            groups: [
              {
                reason: 'symlink',
                count: 3,
                message: '符号链接，nagare 不跟随',
                recovery: '把链接指向的真实路径直接添加进库，或把软链换成硬链接',
                samples: ['/Users/you/Movies/Anime/Season1'],
              },
            ],
          },
        },
      ],
    }
    stubFetch(SETTINGS, allDropped)
    const { container, unmount } = await mount(<RouterProvider router={router} />)

    expect(getByRole(container, 'heading', { level: 2, name: '没有发现视频文件' })).toBeDefined()
    expect(getByText(container, /3 项都被跳过了/)).toBeDefined()
    // 空库时原因就是这一页的全部内容，所以默认展开；同一条信息不该在一屏里出现两次
    const counts = container.textContent?.match(/3 项没有进库/g) ?? []
    expect(counts).toHaveLength(1)
    expect(getByText(container, '3 项没有进库').closest('details')?.open).toBe(true)
    await unmount()
  })

  it('什么都没跳过时完全不出这一块 —— 每次扫描都吓用户一跳是另一种病', async () => {
    stubFetch()
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(container.textContent).not.toContain('没有进库')
    await unmount()
  })

  /**
   * 回写账号失败必须在界面上说出来。
   *
   * 这条守的是一个特别容易复发的形态：失败发生在 mpv 已经退出【之后】，
   * 那条路径上原本只有一句 log.Printf —— 用户面前什么都不会变，
   * 他以为这一集记上了，下次打开网站才发现没有。
   */
  it('看完没能同步到账号时，界面要说清是哪一集、以及怎么办', async () => {
    stubFetch(SETTINGS, LIBRARY, {
      playing: false,
      sync: {
        state: 'failed',
        title: '葬送的芙莉莲',
        episode: 3,
        reason: 'animego 服务暂不可达',
        recovery: '确认网络后重看这一集的结尾，会自动再试一次',
      },
    })
    const { container, unmount } = await mount(<RouterProvider router={router} />)

    const alerts = statuses(container)
    const sync = alerts.find((t) => t.includes('animego 账号'))
    expect(sync, `没有任何横幅提到同步失败，实际横幅：${JSON.stringify(alerts)}`).toBeDefined()
    // 是哪一集 —— 只说「同步失败」用户无从下手
    expect(sync).toContain('葬送的芙莉莲')
    expect(sync).toContain('第3集')
    expect(sync).toContain('animego 服务暂不可达')
    // 恢复动作：另起一行
    expect(getByText(container, /^→ 确认网络后/).textContent).toContain('再试一次')
    await unmount()
  })

  it('没有同步失败时不出这条横幅', async () => {
    stubFetch()
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const alerts = statuses(container)
    expect(alerts.filter((t) => t.includes('animego 账号'))).toEqual([])
    await unmount()
  })

  it('横幅上点「重试」：带 token 头发补发请求，结果说在状态行上，横幅按最新状态消失', async () => {
    let status: PlayerStatus = {
      playing: false,
      sync: { state: 'failed', title: '葬送的芙莉莲', episode: 3, reason: '服务暂不可达', recovery: '确认网络后点「重试」', pending: 1 },
    }
    const fetchMock = stubFetch(SETTINGS, LIBRARY, status)
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      const path = new URL(url, 'http://127.0.0.1').pathname
      let data: unknown
      if (path === '/api/player/sync/retry') {
        status = { playing: false }
        data = { synced: 1, remaining: 0 }
      } else {
        data = path === '/api/library' ? LIBRARY : path === '/api/settings' ? SETTINGS : path === '/api/update' ? UPDATE : status
      }
      void init
      return new Response(JSON.stringify({ success: true, data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    })
    const { container, unmount } = await mount(<RouterProvider router={router} />)

    const retry = queryByRole(container, 'button', { name: '重试' })
    expect(retry, '会再试的同步失败要给「重试」').not.toBeNull()
    await act(async () => { retry!.click() })
    for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve() })

    const posts = fetchMock.mock.calls.filter(([input]) => String(input).includes('/api/player/sync/retry'))
    expect(posts).toHaveLength(1)
    const init = posts[0]![1] as RequestInit
    expect(init.method).toBe('POST')
    expect(new Headers(init.headers).get('X-Nagare-Token')).toBe('9f86d081884c7d659a2feaa0c55ad015')
    expect(statuses(container).some((text) => text.includes('补记上 1 集'))).toBe(true)
    expect(queryByRole(container, 'button', { name: '重试' })).toBeNull()
    await unmount()
  })

  it('库为空时不显示「重新扫描」「上次扫描」，mpv 缺失也只在引导卡里讲一次', async () => {
    stubFirstRun(MPV_MISSING)

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(container.textContent).not.toContain('重新扫描')
    expect(container.textContent).not.toContain('上次扫描')
    // 顶部那条长警示与引导卡讲的是同一件事：引导卡之外不再有讲 mpv 的警示
    const guide = onboarding(container)!
    const outside = getAllByRole(container, 'alert').filter((el) => !guide.contains(el)).map((el) => el.textContent ?? '')
    expect(outside.filter((text) => text.includes('mpv'))).toEqual([])
    await unmount()
  })

  it('mpv 缺失：安装命令与「重新检测」就在引导卡里，切回页面会自动再检测', async () => {
    const mock = stubFirstRun(MPV_MISSING, SETTINGS.mpv)

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const mpv = step(container, '播放器 mpv')
    expect(mpv.textContent).toContain('brew install mpv')
    expect(isReady(mpv)).toBe(false)
    // 不再跳去设置页：新用户跳走之后很难自己找回首页
    expect(getAllByRole(mpv, 'link').map((a) => a.getAttribute('href'))).not.toContain('/settings#player')

    // 用户在终端装完、切回浏览器：窗口重新获得焦点时自动检测，绿点亮起
    await act(async () => { window.dispatchEvent(new Event('focus')) })
    await settle()
    expect(calledPaths(mock)).toContain('/api/mpv/detect')
    expect(isReady(step(container, '播放器 mpv'))).toBe(true)
    await unmount()
  })

  it('「浏览…」逐层选目录，点「添加这个文件夹」直接添加，不用手敲绝对路径', async () => {
    const mock = stubFirstRun()

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    await act(async () => { button(container, '浏览…').click() })
    await settle()
    // 常用位置不能直接添加：先点进具体的文件夹
    expect(button(container, '添加这个文件夹').disabled).toBe(true)
    // 常用位置的按钮名字里带着完整路径（「主目录 /Users/you」）
    await act(async () => { button(container, /^主目录/).click() })
    await settle()
    await act(async () => { button(container, 'Anime').click() })
    await settle()
    await act(async () => { button(container, '添加这个文件夹').click() })
    await settle()

    const add = mock.mock.calls.find(([input]) => String(input).includes('/api/library/folders'))
    expect(add).toBeDefined()
    expect(JSON.parse(String((add![1] as RequestInit).body))).toEqual({ path: '/Users/you/Anime' })
    await unmount()
  })

  it('只用磁力的人可以跳过本地文件夹；mpv 没装好之前不给跳过', async () => {
    stubFirstRun()

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    await act(async () => { button(container, '先不添加本地文件夹').click() })
    expect(onboarding(container)).toBeNull()
    const empty = getByRole(container, 'region', { name: '本地媒体库还是空的' })
    // 想加的时候表单就在这里
    expect(getByRole(empty, 'textbox', { name: '文件夹绝对路径' })).toBeDefined()

    await act(async () => { button(container, '显示新手引导').click() })
    expect(onboarding(container)).not.toBeNull()
    await unmount()

    stubFirstRun(MPV_MISSING)
    const second = await mount(<RouterProvider router={router} />)
    expect(queryByRole(second.container, 'button', { name: '先不添加本地文件夹' })).toBeNull()
    await second.unmount()
  })

  it('有磁力来源但引擎没起来时不能说「已就绪」，要指向磁力播放设置', async () => {
    stubFirstRun({ ...SETTINGS, torrent: { ...SETTINGS.torrent!, enabled: false } }, SETTINGS.mpv, true)

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    await settle()
    const magnet = step(container, '磁力搜索')
    expect(magnet.textContent).toContain('磁力引擎没有启动')
    expect(isReady(magnet)).toBe(false)
    expect(getByRole(magnet, 'link', { name: '查看磁力播放设置' }).getAttribute('href')).toBe('/settings#torrent')
    await unmount()
  })

  it('跳过记在本机：刷新后仍是简洁空库；但 mpv 没装好时照样显示引导', async () => {
    window.localStorage.setItem(SKIP_FOLDERS_KEY, '1')
    stubFirstRun()
    const first = await mount(<RouterProvider router={router} />)
    await settle()
    expect(queryByRole(first.container, 'region', { name: '本地媒体库还是空的' })).not.toBeNull()
    expect(onboarding(first.container)).toBeNull()
    await first.unmount()

    stubFirstRun(MPV_MISSING)
    const second = await mount(<RouterProvider router={router} />)
    await settle()
    expect(onboarding(second.container)).not.toBeNull()
    expect(queryByRole(second.container, 'region', { name: '本地媒体库还是空的' })).toBeNull()
    await second.unmount()
  })

  it('找到了 mpv 却用不了（版本过低）时，原因写在引导卡里，而不是只给安装命令', async () => {
    const tooOld: SettingsData = {
      ...MPV_MISSING,
      mpv: { ...MPV_MISSING.mpv, hint: 'mpv 版本过低：当前 0.29.1，最低要求 0.32.0。请用发行版包管理器升级 mpv' },
    }
    stubFirstRun(tooOld)

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    await settle()
    expect(getByRole(step(container, '播放器 mpv'), 'alert').textContent).toContain('版本过低')
    await unmount()
  })

  it('引导屏必须给出不需要本地文件夹的出路', async () => {
    // 这是这个页面存在的理由：磁力那条路与本地文件夹互相独立，
    // 旧空态只有一个「交出文件夹」表单，等于把可选项摆成了唯一入口。
    stubFirstRun()

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const guide = onboarding(container)!
    expect(guide.textContent).toContain('两条路互相独立')
    const hrefs = getAllByRole(guide, 'link').map((a) => a.getAttribute('href'))
    // 未配规则仓库时先去设置填地址；配好之后那颗按钮指向 /search（见组件）
    expect(hrefs).toContain('/settings#sources')
    // 必需项只有 mpv：其余三张卡都标「可选」
    const tags = getAllByText(guide, /^(必需|可选)$/).map((tag) => tag.textContent)
    expect(tags).toEqual(['必需', '可选', '可选', '可选'])
    await unmount()
  })
})
