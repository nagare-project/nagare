// @vitest-environment jsdom
// 整页冒烟：真实路由 + 真实 apiFetch，只在 fetch 边界打桩。
// 证明「路由 → 页面 → hooks → 组件」整条装配线能对着契约数据渲染出界面。
import { RouterProvider } from '@tanstack/react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { LibraryData, PlayerStatus, SettingsData, UpdateView } from '../lib/endpoints'
import { TOKEN_STORAGE_KEY } from '../lib/token'
import { router } from '../routes'
import { mount } from '../test/harness'
import { installLocalStorage } from '../test/storage'

const LIBRARY: LibraryData = {
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
function stubFetch(settings: SettingsData = SETTINGS): ReturnType<typeof vi.fn> {
  const impl = async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const path = new URL(url, 'http://127.0.0.1').pathname
    const payload: Record<string, unknown> = {
      '/api/library': LIBRARY,
      '/api/settings': settings,
      '/api/player/status': PLAYER,
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

    // 簇卡片：标题 + 季徽标 + 集数；置信度 0.93 不出「低置信」
    expect(container.textContent).toContain('葬送的芙莉莲')
    expect(container.textContent).toContain('第1季')
    expect(container.textContent).toContain('2 集')
    expect(container.textContent).not.toContain('低置信')

    // 两行剧集 + 看到一半那行的进度条
    expect(container.querySelectorAll('.ep-row')).toHaveLength(2)
    expect(container.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow')).toBe('50')

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
    expect(alert?.querySelector('a')?.getAttribute('href')).toBe('/settings')
    expect(alert?.querySelector('a')?.textContent).toContain('去设置安装 mpv')
    await unmount()
  })

  /** 库为空 + 未配规则仓库：最"空"的首次运行状态 */
  function stubFirstRun(): void {
    const empty: LibraryData = { folders: [], clusters: [], scannedAt: null }
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

  it('引导屏必须给出不需要本地文件夹的出路', async () => {
    // 这是这个页面存在的理由：磁力那条路与本地文件夹互相独立，
    // 旧空态只有一个「交出文件夹」表单，等于把可选项摆成了唯一入口。
    stubFirstRun()

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(container.textContent).toContain('两条路互相独立')
    const hrefs = [...container.querySelectorAll('.onboard a')].map((a) => a.getAttribute('href'))
    // 未配规则仓库时先去设置填地址；配好之后那颗按钮指向 /search（见组件）
    expect(hrefs).toContain('/settings')
    // 必需项只有 mpv：其余三张卡都标「可选」
    const tags = [...container.querySelectorAll('.onboard-step .badge')].map((b) => b.textContent)
    expect(tags).toEqual(['必需', '可选', '可选', '可选'])
    await unmount()
  })
})
