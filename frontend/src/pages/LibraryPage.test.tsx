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
import { installLocalStorage } from '../test/storage'

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
  scannedAt: 1_756_500_000,
}

const SETTINGS: SettingsData = {
  version: '0.1.0',
  platform: 'darwin',
  arch: 'arm64',
  dataDir: '/Users/you/Library/Application Support/nagare',
  logPath: '/Users/you/Library/Application Support/nagare/nagare.log',
  mpv: { found: true, version: '0.38.0', path: '/opt/homebrew/bin/mpv', source: 'path' },
  animego: { loggedIn: false, baseUrl: 'https://animego.example' },
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
  // apiFetch 会读 sessionStorage 里的 token（形状必须合法）
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '9f86d081884c7d659a2feaa0c55ad015')
  window.history.replaceState(null, '', '/')
  // jsdom 没实现 scrollTo，TanStack Router 挂载时会调一次；打桩消掉告警噪音
  vi.stubGlobal('scrollTo', vi.fn())
  // 根布局的 UpdateBanner 会读 localStorage（Node 自带的全局壳在测试里不可用）
  installLocalStorage()
})

afterEach(() => {
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

describe('LibraryPage（整页冒烟）', () => {
  it('看完一集、下一集尚未开始的作品仍归入在看', async () => {
    const library = structuredClone(LIBRARY)
    library.clusters[0]!.groups[0]!.items[0]!.progress = { positionSec: 1420, durationSec: 1420, completed: true }
    stubFetch(SETTINGS, library)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const filters = Array.from(container.querySelectorAll<HTMLButtonElement>('.collection-filter'))
    await act(async () => { filters.find((button) => button.textContent === '在看')!.click() })
    expect(container.querySelector('.poster-title')?.textContent).toBe('葬送的芙莉莲')
    await act(async () => { filters.find((button) => button.textContent === '已看完')!.click() })
    expect(container.querySelector('.poster')).toBeNull()
    expect(container.querySelector('.collection-empty')?.textContent).toContain('没有符合条件的作品')
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
    expect(document.querySelector('.update-banner')).toBeNull()

    // 页头与 mpv 状态点
    expect(container.querySelector('.page-title')?.textContent).toContain('媒体库')
    expect(container.querySelector('.mpv-dot--ok')).not.toBeNull()
    expect(container.querySelector('.alert-warn')).toBeNull()

    // 海报卡：标题 + 季 + 集数；置信度 0.93 不出「低置信」
    const poster = container.querySelector('.poster')
    expect(poster?.querySelector('.poster-title')?.textContent).toBe('葬送的芙莉莲')
    expect(poster?.querySelector('.poster-meta')?.textContent).toContain('第 1 季')
    expect(poster?.querySelector('.poster-meta')?.textContent).toContain('2 集')
    expect(container.textContent).not.toContain('低置信')

    // 剧集列表【不】在库页上：它住在 /anime/$clusterKey，卡片链过去
    expect(container.querySelectorAll('.ep-row')).toHaveLength(0)
    expect(poster?.querySelector('a')?.getAttribute('href')).toBe('/anime/frieren')

    // 未在播放：不出现在播条
    expect(container.querySelector('.np-bar')).toBeNull()

    await unmount()
  })

  it('mpv 未找到：状态点变红，提示条里给出「去设置安装 mpv」链接', async () => {
    stubFetch(MPV_MISSING)
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(container.querySelector('.mpv-dot--missing')).not.toBeNull()
    const alert = container.querySelector('.alert-warn')
    expect(alert?.textContent).toContain('未检测到 mpv')
    expect(alert?.querySelector('a')?.getAttribute('href')).toBe('/settings#player')
    expect(alert?.querySelector('a')?.textContent).toContain('去设置安装 mpv')
    await unmount()
  })

  /** 库为空 + 未配规则仓库：最"空"的首次运行状态 */
  function stubFirstRun(): void {
    const empty: LibraryData = { folders: [], clusters: [], continueWatching: [], scannedAt: null }
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
      const path = new URL(url, 'http://127.0.0.1').pathname
      const data =
        path === '/api/library'
          ? empty
          : path === '/api/settings'
            ? SETTINGS
            : path === '/api/sources'
              ? noSources
              : path === '/api/source-plugin'
                ? { config: { enabled: false, executable: '', root: '' }, status: { phase: 'disabled' }, sources: [] }
                : path === '/api/update'
                  ? UPDATE
                  : PLAYER
      return new Response(JSON.stringify({ success: true, data }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    vi.stubGlobal('fetch', vi.fn(impl))
  }

  it('库为空时显示引导屏，四个准备项齐全', async () => {
    stubFirstRun()

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(container.querySelector('.onboard')).not.toBeNull()
    // 四项：mpv / 本地文件夹 / 磁力搜索 / 账号
    expect(container.querySelectorAll('.onboard-step')).toHaveLength(4)
    // 添加文件夹的表单仍然内联在这里，不必点进另一个页面
    expect(container.querySelector('input[name="path"]')).not.toBeNull()
    // mpv 已装 → 绿点；其余三项都是待办
    expect(container.querySelectorAll('.onboard-dot--done')).toHaveLength(1)
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

    const drops = container.querySelector('.drops')
    expect(drops, '扫描跳过了东西，界面上却没有任何痕迹').not.toBeNull()
    expect(drops?.querySelector('.drops-count')?.textContent).toBe('3 项没有进库')
    // 收起状态下也要看得见原因 —— 只说「3 项没有进库」等于什么都没说
    expect(drops?.querySelector('.drops-why')?.textContent).toContain('符号链接')

    // 恢复动作：这才是这块东西存在的理由
    const recoveries = [...container.querySelectorAll('.drops-recovery')].map((e) => e.textContent)
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

    expect(container.querySelector('.page-notice-title')?.textContent).toBe('没有发现视频文件')
    expect(container.querySelector('.page-notice-copy')?.textContent).toContain('3 项都被跳过了')
    // 空库时原因就是这一页的全部内容，所以默认展开
    const drops = container.querySelector('.drops')
    expect(drops?.hasAttribute('open')).toBe(true)
    // 同一条信息不该在一屏里出现两次
    expect(container.querySelectorAll('.drops')).toHaveLength(1)
    await unmount()
  })

  it('什么都没跳过时完全不出这一块 —— 每次扫描都吓用户一跳是另一种病', async () => {
    stubFetch()
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(container.querySelector('.drops')).toBeNull()
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

    const alerts = [...container.querySelectorAll('.alert-warn')].map((e) => e.textContent ?? '')
    const sync = alerts.find((t) => t.includes('animego 账号'))
    expect(sync, `没有任何横幅提到同步失败，实际横幅：${JSON.stringify(alerts)}`).toBeDefined()
    // 是哪一集 —— 只说「同步失败」用户无从下手
    expect(sync).toContain('葬送的芙莉莲')
    expect(sync).toContain('第3集')
    expect(sync).toContain('animego 服务暂不可达')
    // 恢复动作
    expect(container.querySelector('.alert-warn-recovery')?.textContent).toContain('再试一次')
    await unmount()
  })

  it('没有同步失败时不出这条横幅', async () => {
    stubFetch()
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const alerts = [...container.querySelectorAll('.alert-warn')].map((e) => e.textContent ?? '')
    expect(alerts.filter((t) => t.includes('animego 账号'))).toEqual([])
    await unmount()
  })

  it('引导屏必须给出不需要本地文件夹的出路', async () => {
    // 这是这个页面存在的理由：磁力那条路与本地文件夹互相独立，
    // 旧空态只有一个「交出文件夹」表单，等于把可选项摆成了唯一入口。
    stubFirstRun()

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(container.textContent).toContain('两条路互相独立')
    const hrefs = [...container.querySelectorAll('.onboard a')].map((a) => a.getAttribute('href'))
    // 未配规则仓库时先去设置填地址；配好之后那颗按钮指向 /search（见组件）
    expect(hrefs).toContain('/settings#sources')
    // 必需项只有 mpv：其余三张卡都标「可选」
    const tags = [...container.querySelectorAll('.onboard-step .badge')].map((b) => b.textContent)
    expect(tags).toEqual(['必需', '可选', '可选', '可选'])
    await unmount()
  })
})
