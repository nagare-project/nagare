// @vitest-environment jsdom
import { act } from 'react'
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '../routes'
import { accessibleName, getAllByRole, getByRole, getByText, queryByRole } from '../test/queries'
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
/** 后台形态与平台（「后台与退出」按它们措辞） */
let background: { mode: string } = { mode: 'dock' }
let platform = 'darwin'

beforeEach(() => {
  coverCache = null
  background = { mode: 'dock' }
  platform = 'darwin'
  installLocalStorage()
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '00000000000000000000000000000000')
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = new URL(String(input), 'http://localhost').pathname
    const data: Record<string, unknown> = {
      '/api/settings': { ...settings, coverCache, background, platform },
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

/** 设置分类导航里标着当前页的链接（只应有一个） */
function currentNavLinks(container: HTMLElement): string[] {
  const nav = getByRole(container, 'navigation', { name: '设置分类' })
  return getAllByRole(nav, 'link').filter((a) => a.getAttribute('aria-current') === 'page').map(accessibleName)
}

/** 当前看得见的那一区的标题（hidden 的分区读屏读不到，查询也查不到） */
function visibleSectionTitle(container: HTMLElement): string {
  return getByRole(container, 'heading', { level: 2 }).textContent ?? ''
}

describe('设置分区', () => {
  it('分类按三组排：账号在最上面，下面是「媒体库与 NAS」「播放」「关于」；扩展是另一页的入口', async () => {
    const { container, unmount } = await open('/settings')
    const nav = getByRole(container, 'navigation', { name: '设置分类' })
    const groups = getAllByRole(nav, 'list').map(accessibleName).filter((name) => name !== '')
    expect(groups).toEqual(['媒体库与 NAS', '播放', '关于'])
    expect(getAllByRole(getByRole(nav, 'list', { name: '播放' }), 'link').map(accessibleName)).toEqual(['播放器', '画质增强', '磁力播放', '来源', '扩展'])
    expect(getAllByRole(getByRole(nav, 'list', { name: '关于' }), 'link').map(accessibleName)).toEqual(['更新', '后台与退出', '关于 nagare'])
    expect(getByRole(nav, 'link', { name: '扩展' }).getAttribute('href')).toBe('/extensions')
    await unmount()
  })

  it('没有 #hash 时打开账号，导航只有它一个是当前页；认不出的 #hash 也落到账号', async () => {
    for (const path of ['/settings', '/settings#nonsense']) {
      const { container, unmount } = await open(path)
      expect(visibleSectionTitle(container)).toBe('账号')
      expect(currentNavLinks(container)).toEqual(['账号'])
      await unmount()
    }
  })

  it('安装恢复链接直接打开播放器：mpv 面板与安装命令都在，只有一个分类被选中', async () => {
    const { container, unmount } = await open('/settings#player')
    expect(visibleSectionTitle(container)).toBe('播放器')
    expect(getByRole(container, 'heading', { level: 3, name: 'mpv' })).not.toBeNull()
    expect(getByRole(container, 'button', { name: '复制' }).getAttribute('aria-describedby')).not.toBeNull()
    expect(getByRole(container, 'button', { name: '重新检测' })).not.toBeNull()
    expect(currentNavLinks(container)).toEqual(['播放器'])
    await unmount()
  })

  it('点导航切换分区：地址换成那一区的 #hash，焦点落到新分区的标题上', async () => {
    const { container, router, unmount } = await open('/settings')
    const link = getByRole(getByRole(container, 'navigation', { name: '设置分类' }), 'link', { name: '磁力播放' })
    await act(async () => { link.click() })
    await act(async () => {})
    expect(router.state.location.hash).toBe('torrent')
    expect(visibleSectionTitle(container)).toBe('磁力播放')
    expect(document.activeElement).toBe(getByRole(container, 'heading', { level: 2, name: '磁力播放' }))
    expect(currentNavLinks(container)).toEqual(['磁力播放'])
    await unmount()
  })

  it('磁力播放分区能到磁力任务与磁力搜索', async () => {
    const { container, unmount } = await open('/settings#torrent')
    const shortcuts = getByRole(container, 'navigation', { name: '磁力任务与搜索' })
    const hrefs = getAllByRole(shortcuts, 'link').map((a) => a.getAttribute('href'))
    expect(hrefs).toEqual(['/torrents', '/search'])
    expect(getByRole(container, 'switch', { name: '持续做种' })).not.toBeNull()
    await unmount()
  })

  it('「关于 nagare」里显示封面缓存的占用；没有封面缓存时不出这一栏', async () => {
    coverCache = { bytes: 5 * 1024 * 1024, count: 42, maxBytes: 300 * 1024 * 1024 }
    const shown = await open('/settings#about')
    const about = getByRole(shown.container, 'region', { name: '关于 nagare' })
    expect(getByRole(about, 'heading', { name: '封面缓存' })).not.toBeNull()
    expect(about.textContent).toContain('5 MB · 42 张')
    expect(getByRole(about, 'link', { name: 'AGPL-3.0 ↗' }).getAttribute('href')).toContain('/LICENSE')
    await shown.unmount()

    for (const absent of [null, undefined]) {
      coverCache = absent
      const hidden = await open('/settings#about')
      try {
        expect(getByRole(hidden.container, 'heading', { name: '版本与数据' })).not.toBeNull()
        expect(queryByRole(hidden.container, 'heading', { name: '封面缓存' })).toBeNull()
      } finally {
        await hidden.unmount()
      }
    }
  })

  it('「后台与退出」说明在哪里找到它，并给出退出', async () => {
    const { container, unmount } = await open('/settings#background')
    const section = getByRole(container, 'region', { name: '后台与退出' })
    expect(section.textContent).toContain('Dock 图标与菜单栏图标')
    expect(section.textContent).toContain('点 Dock 图标可以重新打开界面')
    expect(getByRole(section, 'button', { name: '退出 nagare' })).not.toBeNull()
    await unmount()
  })

  // 每种后台形态各说各的「在哪里找它」：说错了，用户会去找一个不存在的图标
  it.each([
    { mode: 'menubar', os: 'darwin', where: '菜单栏图标', hint: '从菜单栏右上角的「流」图标可以重新打开界面或退出', quit: 'macOS 也可以从菜单栏图标退出' },
    { mode: 'tray', os: 'windows', where: '系统托盘图标', hint: '可能折叠在任务栏的「^」里', quit: 'Windows 也可以从托盘图标退出' },
    { mode: 'tray', os: 'linux', where: '系统托盘图标', hint: '桌面不显示托盘时，用设置页里的「退出 nagare」', quit: 'Linux 没有托盘图标，退出请用这里' },
    { mode: 'none', os: 'linux', where: '没有图标（命令行或无托盘模式）', hint: '在启动它的终端按 Ctrl+C', quit: 'Linux 没有托盘图标，退出请用这里' },
  ])('「后台与退出」在 $mode（$os）形态下说清在哪里找它、怎么退出', async ({ mode, os, where, hint, quit }) => {
    background = { mode }
    platform = os
    const { container, unmount } = await open('/settings#background')
    const section = getByRole(container, 'region', { name: '后台与退出' })
    const panel = getByRole(section, 'region', { name: '后台运行' })
    expect(getByText(panel, where).tagName).toBe('DD')
    expect(panel.textContent).toContain(hint)
    expect(panel.textContent).not.toContain('Dock')
    expect(getByRole(section, 'region', { name: '退出 nagare' }).textContent).toContain(quit)
    expect(getByRole(section, 'button', { name: '退出 nagare' })).not.toBeNull()
    await unmount()
  })

  it('点当前所在的分区：地址不变，焦点移到这一区的标题上（读屏念出还在哪一区）', async () => {
    const { container, router, unmount } = await open('/settings#torrent')
    const link = getByRole(getByRole(container, 'navigation', { name: '设置分类' }), 'link', { name: '磁力播放' })
    link.focus()
    await act(async () => { link.click() })
    await act(async () => {})
    expect(router.state.location.hash).toBe('torrent')
    expect(document.activeElement).toBe(getByRole(container, 'heading', { level: 2, name: '磁力播放' }))
    await unmount()
  })

  it('带修饰键点导航（新标签页打开）：本页不切分区、焦点不动；之后从别的路径切过去也不抢焦点', async () => {
    const { container, router, unmount } = await open('/settings')
    const nav = getByRole(container, 'navigation', { name: '设置分类' })
    const link = getByRole(nav, 'link', { name: '关于 nagare' })
    link.focus()
    // 链接的默认动作（开新标签页）在 jsdom 里会变成「不支持的跳转」：等页面自己的处理跑完再拦掉
    const stopDefault = (event: Event) => event.preventDefault()
    document.addEventListener('click', stopDefault)
    try {
      for (const modifier of ['metaKey', 'ctrlKey', 'shiftKey'] as const) {
        await act(async () => { link.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0, [modifier]: true })) })
        expect(router.state.location.hash).toBe('')
        expect(visibleSectionTitle(container)).toBe('账号')
        expect(document.activeElement).toBe(link)
      }
    } finally {
      document.removeEventListener('click', stopDefault)
    }
    // 那几下没有登记「切过去后移焦点」：后退 / 前进之类别的路径切到这一区时，焦点留在原处
    await act(async () => { await router.navigate({ to: '/settings', hash: 'about' }) })
    expect(visibleSectionTitle(container)).toBe('关于 nagare')
    expect(document.activeElement).toBe(link)
    await unmount()
  })

  it('切换分类保留登录草稿，并在登录前说明会话互斥的限制', async () => {
    const { container, router, unmount } = await open('/settings#account')
    const email = getByRole(container, 'textbox', { name: '邮箱' }) as HTMLInputElement
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(email, 'test@example.com')
      email.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(getByRole(getByRole(container, 'main'), 'note', { name: /不能同时保持登录/ })).not.toBeNull()
    expect(getByRole(container, 'button', { name: '登录' })).not.toBeNull()
    await act(async () => { await router.navigate({ to: '/settings', hash: 'folders' }) })
    expect(visibleSectionTitle(container)).toBe('媒体文件夹')
    expect(getByRole(container, 'heading', { level: 3, name: '已添加的文件夹' })).not.toBeNull()
    await act(async () => { await router.navigate({ to: '/settings', hash: 'account' }) })
    expect((getByRole(container, 'textbox', { name: '邮箱' }) as HTMLInputElement).value).toBe('test@example.com')
    await unmount()
  })
})
