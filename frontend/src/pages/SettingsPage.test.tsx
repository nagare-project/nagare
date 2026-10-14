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

beforeEach(() => {
  installLocalStorage()
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '00000000000000000000000000000000')
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = new URL(String(input), 'http://localhost').pathname
    const data: Record<string, unknown> = {
      '/api/settings': settings,
      '/api/library': { folders: [], clusters: [], continueWatching: [], scannedAt: null },
      '/api/sources': { sources: [], rules: { remoteUrl: '', localDir: '', dir: '', loaded: 0, errors: [], lastLoadedAt: null, lastSyncAt: null } },
      '/api/source-plugin': { config: { enabled: false, executable: '', root: '' }, status: { phase: 'disabled' }, sources: [], installed: [], installSupported: false },
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

describe('从地址安装的来源', () => {
  const record = {
    id: 'demo', name: '示例来源', version: '3', pack: false, url: 'https://example.invalid/sources/demo.json',
    sha256: 'ab'.repeat(32), installedAt: '2026-01-02T03:04:05Z',
    sources: [{ id: 'demo', name: '示例来源', kind: 'bt', version: '3', format: 'json' }],
  }
  const pack = {
    id: 'demo-pack', name: '示例来源包', version: 'abc123', pack: true, url: 'https://example.invalid/packs/demo.json',
    sha256: 'cd'.repeat(32), installedAt: '2026-01-02T03:04:05Z',
    sources: [
      { id: 'alpha', name: '甲来源', kind: 'bt', version: '1', format: 'json' },
      { id: 'beta', name: '乙来源', kind: 'web', version: '1', format: 'json' },
    ],
  }
  const readyView = (installed: unknown[], installSupported = true) => ({
    config: { enabled: true, executable: '/opt/nagare-source', root: '' },
    status: { phase: 'ready', manifest: { id: 'nagare-source', name: 'Nagare Source', version: '0.3.0', protocolVersions: [1], sourceSchemaVersions: [1], capabilities: [] } },
    sources: [], installed, installSupported,
  })

  function stubPlugin(view: unknown, onDelete?: (path: string) => { status: number; body: unknown }) {
    const fallback = globalThis.fetch
    const deletes: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = new URL(String(input), 'http://localhost').pathname
      if (init?.method === 'DELETE') {
        deletes.push(path)
        const reply = onDelete?.(path) ?? { status: 200, body: { success: true, data: readyView([]) } }
        return new Response(JSON.stringify(reply.body), { status: reply.status, headers: { 'Content-Type': 'application/json' } })
      }
      if (path === '/api/source-plugin') {
        return new Response(JSON.stringify({ success: true, data: view }), { headers: { 'Content-Type': 'application/json' } })
      }
      return fallback(input, init)
    }))
    return deletes
  }

  const section = (container: HTMLElement) => container.querySelector<HTMLElement>('.source-plugin-installed-head')!.parentElement!
  const button = (root: HTMLElement, text: string) =>
    [...root.querySelectorAll<HTMLButtonElement>('button')].find(item => item.textContent === text)

  it('列出出处并提供安装入口，二次确认后才卸载', async () => {
    const deletes = stubPlugin(readyView([record]))
    const { container, unmount } = await open('/settings#sources')
    const list = section(container)
    expect(list.textContent).toContain('从地址安装的来源 · 1')
    expect(list.textContent).toContain('demo · v3 · example.invalid')
    expect(list.querySelector('a[href="/sources/install"]')?.textContent).toBe('从地址安装')

    await act(async () => { button(list, '卸载')!.click() })
    expect(deletes).toHaveLength(0)
    await act(async () => { button(list, '取消')!.click() })
    await act(async () => { button(list, '卸载')!.click() })
    await act(async () => { button(list, '确认卸载')!.click() })
    expect(deletes).toEqual(['/api/source-plugin/sources/installed/demo'])
    expect(section(container).textContent).toContain('从地址安装的来源 · 0')
    expect(section(container).textContent).not.toContain('示例来源')
    await unmount()
  })

  it('来源包显示成员，整体卸载', async () => {
    const deletes = stubPlugin(readyView([pack]))
    const { container, unmount } = await open('/settings#sources')
    const list = section(container)
    expect(list.textContent).toContain('来源包 · 2 个来源 · vabc123 · example.invalid')
    const members = [...list.querySelectorAll('.source-plugin-pack li')].map(item => item.textContent)
    expect(members).toEqual(['甲来源alpha · BT 磁力', '乙来源beta · 在线播放'])

    await act(async () => { button(list, '卸载')!.click() })
    await act(async () => { button(list, '确认卸载 2 个来源')!.click() })
    expect(deletes).toEqual(['/api/source-plugin/sources/installed/demo-pack'])
    expect(section(container).textContent).not.toContain('示例来源包')
    await unmount()
  })

  it('卸载失败时保留条目并显示原因', async () => {
    stubPlugin(readyView([record]), () => ({ status: 502, body: { success: false, error: '来源插件重启失败，已恢复卸载前的状态' } }))
    const { container, unmount } = await open('/settings#sources')
    const list = section(container)
    await act(async () => { button(list, '卸载')!.click() })
    await act(async () => { button(list, '确认卸载')!.click() })
    expect(list.querySelector('[role="alert"]')?.textContent).toContain('已恢复卸载前的状态')
    expect(list.textContent).toContain('示例来源')
    await unmount()
  })

  it('插件不支持时不给安装入口并说明原因', async () => {
    stubPlugin(readyView([], false))
    const { container, unmount } = await open('/settings#sources')
    const list = section(container)
    expect(list.querySelector('a[href="/sources/install"]')).toBeNull()
    expect(list.textContent).toContain('当前插件版本不支持从地址安装来源')
    await unmount()
  })
})
