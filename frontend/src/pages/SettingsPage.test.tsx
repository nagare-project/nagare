// @vitest-environment jsdom
import { act } from 'react'
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '../routes'
import { mount } from '../test/harness'
import { installLocalStorage } from '../test/storage'
import { TOKEN_STORAGE_KEY } from '../lib/token'

const settings = {
  version: '0.2.0', platform: 'darwin', arch: 'arm64', dataDir: '/config', logPath: '/config/log',
  mpv: { found: false, install: { command: 'brew install mpv', url: 'https://mpv.io/installation/', note: '' } },
  animego: { loggedIn: false, baseUrl: 'https://account.example' },
  background: { mode: 'dock' }, player: { anime4k: 'off' },
  torrent: { enabled: true, seeding: false, trackers: [], useDefaultTrackers: true, defaultTrackers: [], portForwarding: true, listenPort: 6881, cacheDir: '/cache', cacheBytes: 0, downloadDir: '/Users/you/Downloads/nagare', downloadDirIsDefault: true },
}

/** 设置载荷里的封面缓存占用；null = 这次运行没有封面缓存；undefined = 旧后端没有这个字段 */
let coverCache: { bytes: number; count: number; maxBytes: number } | null | undefined = null

beforeEach(() => {
  coverCache = null
  installLocalStorage()
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '00000000000000000000000000000000')
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = new URL(String(input), 'http://localhost').pathname
    const data: Record<string, unknown> = {
      '/api/settings': { ...settings, coverCache },
      '/api/library': { folders: [], clusters: [], continueWatching: [], scannedAt: null },
      '/api/sources': { sources: [], rules: { remoteUrl: '', localDir: '', dir: '', loaded: 0, errors: [], lastLoadedAt: null, lastSyncAt: null } },
      '/api/source-plugin': { config: { enabled: false, executable: '', root: '' }, status: { phase: 'disabled' }, sources: [] },
      '/api/update': { enabled: false, current: '0.2.0', latest: '', available: false, url: '', checkedAt: null, error: '', selfUpdate: { supported: false, channel: 'unknown' } },
    }
    return new Response(JSON.stringify({ success: true, data: data[path] }), { headers: { 'Content-Type': 'application/json' } })
  }))
})
afterEach(() => { vi.unstubAllGlobals(); window.sessionStorage.clear() })

async function open(path: string) {
  const router = createAppRouter(createMemoryHistory({ initialEntries: [path] }))
  await router.load()
  return { router, ...await mount(<RouterProvider router={router} />) }
}

describe('设置分区', () => {
  it('安装恢复链接直接打开播放器，只有一个分类被选中', async () => {
    const { container, unmount } = await open('/settings#player')
    expect(container.querySelector('.settings-section:not([hidden]) #mpv-heading')).not.toBeNull()
    const selected = container.querySelectorAll('.settings-nav-item[aria-current="page"]')
    expect(selected).toHaveLength(1)
    expect(selected[0]?.textContent).toBe('媒体播放器')
    await unmount()
  })

  it('「关于」里显示封面缓存的占用；没有封面缓存时不出这一栏', async () => {
    coverCache = { bytes: 5 * 1024 * 1024, count: 42, maxBytes: 300 * 1024 * 1024 }
    const shown = await open('/settings#about')
    const about = shown.container.querySelector('.settings-section:not([hidden])')
    expect(about?.querySelector('#cover-cache-heading')).not.toBeNull()
    expect(about?.textContent).toContain('5 MB · 42 张')
    await shown.unmount()

    for (const absent of [null, undefined]) {
      coverCache = absent
      const hidden = await open('/settings#about')
      try {
        expect(hidden.container.querySelector('#about-heading')).not.toBeNull()
        expect(hidden.container.querySelector('#cover-cache-heading')).toBeNull()
      } finally {
        await hidden.unmount()
      }
    }
  })

  it('「扩展」收在设置的分类导航里；磁力播放分区能到磁力任务与磁力搜索', async () => {
    const { container, unmount } = await open('/settings#torrent')
    const nav = container.querySelector('nav[aria-label="设置分类"]')!
    const extensions = [...nav.querySelectorAll('a')].find((a) => a.textContent === '扩展')
    expect(extensions?.getAttribute('href')).toBe('/extensions')
    const section = container.querySelector('.settings-section:not([hidden])')!
    const hrefs = [...section.querySelectorAll('a')].map((a) => a.getAttribute('href'))
    expect(hrefs).toEqual(expect.arrayContaining(['/torrents', '/search']))
    await unmount()
  })

  it('切换分类保留登录草稿，并保留登录前的会话限制说明', async () => {
    const { container, router, unmount } = await open('/settings#account')
    const email = container.querySelector<HTMLInputElement>('#animego-email')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(email, 'test@example.com')
      email.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.querySelector('.settings-section:not([hidden])')?.textContent).toContain('不能同时保持登录')
    await act(async () => { await router.navigate({ to: '/settings', hash: 'folders' }) })
    expect(container.querySelector('.settings-section:not([hidden]) #folders-heading')).not.toBeNull()
    await act(async () => { await router.navigate({ to: '/settings', hash: 'account' }) })
    expect(container.querySelector<HTMLInputElement>('#animego-email')?.value).toBe('test@example.com')
    await unmount()
  })
})
