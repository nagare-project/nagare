// @vitest-environment jsdom
// 顶栏与抽屉：真实路由（memory history），接口一律答 401 —— 页面换成恢复指引，顶栏照常工作。
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { act } from 'react'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '../../routes'
import { mount } from '../../test/harness'
import { installLocalStorage } from '../../test/storage'

// jsdom 没有 <dialog> 的模态方法
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')
beforeAll(() => {
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function (this: HTMLDialogElement) { this.setAttribute('open', '') } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: function (this: HTMLDialogElement) {
    if (!this.hasAttribute('open')) return
    this.removeAttribute('open'); this.dispatchEvent(new Event('close'))
  } })
})
afterAll(() => {
  if (show) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', show)
  else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal')
  if (close) Object.defineProperty(HTMLDialogElement.prototype, 'close', close)
  else Reflect.deleteProperty(HTMLDialogElement.prototype, 'close')
})

beforeEach(() => {
  installLocalStorage()
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ success: false, error: '缺少或错误的鉴权 token' }), {
    status: 401,
    headers: { 'Content-Type': 'application/json' },
  })))
})
afterEach(() => {
  vi.unstubAllGlobals()
})

async function mountAt(...entries: string[]) {
  const router = createAppRouter(createMemoryHistory({ initialEntries: entries, initialIndex: entries.length - 1 }))
  await router.load()
  const view = await mount(<RouterProvider router={router} />)
  return { router, ...view }
}

async function settle(): Promise<void> {
  for (let i = 0; i < 4; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
}

const mainNav = (root: HTMLElement) => root.querySelector<HTMLElement>('nav[aria-label="主导航"]')!
const buttonNamed = (root: ParentNode, name: string) => [...root.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.getAttribute('aria-label') === name)!
const linkNamed = (root: ParentNode, text: string) => [...root.querySelectorAll<HTMLAnchorElement>('a')].find((a) => a.textContent === text)!

describe('顶栏', () => {
  it('主导航是「发现 · 媒体库 · 放送表 · 季度 · 我的追番」，第一屏 / 是发现页，媒体库在 /library', async () => {
    const { container, unmount } = await mountAt('/library')
    const links = [...mainNav(container).querySelectorAll('a')]
    expect(links.map((a) => a.textContent)).toEqual(['发现', '媒体库', '放送表', '季度', '我的追番'])
    expect(links.map((a) => a.getAttribute('href'))).toEqual(['/', '/library', '/schedule', '/seasonal', '/lists'])
    expect(linkNamed(mainNav(container), '媒体库').getAttribute('aria-current')).toBe('page')
    expect(linkNamed(mainNav(container), '发现').getAttribute('aria-current')).toBeNull()
    // 「扩展」不在顶栏里（收进设置）；右侧有设置入口
    expect(container.querySelector('header')?.textContent).not.toContain('扩展')
    expect(container.querySelector('header a[aria-label="设置"]')?.getAttribute('href')).toBe('/settings')
    await unmount()
  })

  it('搜索：放大镜展开成输入框，提交落到发现页 /?q=，焦点回到放大镜', async () => {
    const { router, container, unmount } = await mountAt('/library')
    await act(async () => buttonNamed(container, '搜索作品').click())
    const input = container.querySelector<HTMLInputElement>('header input[aria-label="作品名"]')!
    expect(document.activeElement).toBe(input)

    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '  芙莉莲 ')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => { input.form!.requestSubmit() })
    await settle()

    expect(router.state.location.pathname).toBe('/')
    expect(router.state.location.search).toMatchObject({ q: '芙莉莲' })
    expect(container.querySelector('header input')).toBeNull()
    expect(document.activeElement).toBe(buttonNamed(container, '搜索作品'))
    await unmount()
  })

  it('搜索：Esc 收起并把焦点还给放大镜，地址不变', async () => {
    const { router, container, unmount } = await mountAt('/schedule')
    await act(async () => buttonNamed(container, '搜索作品').click())
    const input = container.querySelector<HTMLInputElement>('header input[aria-label="作品名"]')!
    await act(async () => { input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })) })

    expect(container.querySelector('header input')).toBeNull()
    expect(document.activeElement).toBe(buttonNamed(container, '搜索作品'))
    expect(router.state.location.pathname).toBe('/schedule')
    await unmount()
  })
})

describe('窄屏抽屉', () => {
  it('☰ 打开抽屉；点抽屉里的链接会跳转、关闭抽屉并把焦点还给 ☰', async () => {
    const { router, container, unmount } = await mountAt('/library')
    const menu = buttonNamed(container, '打开导航')
    expect(menu.getAttribute('aria-expanded')).toBe('false')
    await act(async () => menu.click())

    const drawer = container.querySelector<HTMLDialogElement>('dialog.nav-drawer')!
    expect(drawer.open).toBe(true)
    expect(menu.getAttribute('aria-expanded')).toBe('true')
    expect(menu.getAttribute('aria-controls')).toBe(drawer.id)
    // 抽屉里另有磁力任务与设置
    const drawerLinks = [...drawer.querySelectorAll('nav a')].map((a) => a.textContent)
    expect(drawerLinks).toEqual(['发现', '媒体库', '放送表', '季度', '我的追番', '磁力任务', '设置'])

    await act(async () => linkNamed(drawer, '放送表').click())
    await settle()
    expect(router.state.location.pathname).toBe('/schedule')
    expect(drawer.open).toBe(false)
    expect(menu.getAttribute('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(menu)
    await unmount()
  })

  it('✕ 关闭并还焦点；因前进后退而关闭时不把焦点拉回 ☰', async () => {
    const { router, container, unmount } = await mountAt('/library', '/seasonal')
    const menu = buttonNamed(container, '打开导航')
    const drawer = container.querySelector<HTMLDialogElement>('dialog.nav-drawer')!

    await act(async () => menu.click())
    await act(async () => buttonNamed(drawer, '关闭导航').click())
    expect(drawer.open).toBe(false)
    expect(document.activeElement).toBe(menu)

    await act(async () => menu.click())
    buttonNamed(drawer, '关闭导航').focus()
    await act(async () => { router.history.back() })
    await settle()
    expect(router.state.location.pathname).toBe('/library')
    expect(drawer.open).toBe(false)
    expect(document.activeElement).not.toBe(menu)
    await unmount()
  })
})
