// @vitest-environment jsdom
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '../routes'
import { mount } from '../test/harness'
import { installLocalStorage } from '../test/storage'

/** 所有接口都答 401：模拟「这个浏览器没有 token」 */
function stubUnauthorizedFetch(): void {
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubGlobal(
    'fetch',
    vi.fn(async () =>
      new Response(JSON.stringify({ success: false, error: '缺少或错误的鉴权 token' }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  )
}

beforeEach(() => {
  installLocalStorage()
  stubUnauthorizedFetch()
})
afterEach(() => {
  vi.unstubAllGlobals()
})

describe('RootLayout', () => {
  it('没有 token 时任何页面都换成全页恢复指引，而不是裸露接口错误', async () => {
    const router = createAppRouter(createMemoryHistory({ initialEntries: ['/discover'] }))
    await router.load()
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    const text = container.textContent ?? ''
    expect(text).toContain('这个浏览器还没有访问凭证')
    expect(text).toContain('复制登录链接')
    expect(text).not.toContain('缺少或错误的鉴权 token')
    await unmount()
  })
})
