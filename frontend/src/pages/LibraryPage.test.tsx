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

    // 顶栏与 mpv 状态点
    expect(container.querySelector('.topbar-brand')?.textContent).toContain('nagare')
    expect(container.querySelector('.mpv-dot--ok')).not.toBeNull()
    expect(container.querySelector('.mpv-alert')).toBeNull()

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
    const alert = container.querySelector('.mpv-alert')
    expect(alert?.textContent).toContain('未检测到 mpv')
    expect(alert?.querySelector('a')?.getAttribute('href')).toBe('/settings')
    expect(alert?.querySelector('a')?.textContent).toContain('去设置安装 mpv')
    await unmount()
  })

  it('库为空时显示添加文件夹引导', async () => {
    const empty: LibraryData = { folders: [], clusters: [], scannedAt: null }
    const impl = async (input: RequestInfo | URL): Promise<Response> => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      const path = new URL(url, 'http://127.0.0.1').pathname
      const data =
        path === '/api/library'
          ? empty
          : path === '/api/settings'
            ? SETTINGS
            : path === '/api/update'
              ? UPDATE
              : PLAYER
      return new Response(JSON.stringify({ success: true, data }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    vi.stubGlobal('fetch', vi.fn(impl))

    const { container, unmount } = await mount(<RouterProvider router={router} />)
    expect(container.querySelector('.lib-empty')).not.toBeNull()
    expect(container.textContent).toContain('把动漫文件夹交给 nagare')
    expect(container.querySelector('input[name="path"]')).not.toBeNull()
    await unmount()
  })
})
