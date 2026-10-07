// @vitest-environment jsdom
// 作品搜索的路由一层：真实路由（memory history）+ 真实 apiFetch，只在 fetch 边界打桩。
// 守的是 validateSearch、「提交 → 地址栏」「地址栏 → 搜索」这几处胶水 —— 页面级测试把路由钩子换成了替身，测不到它们。
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TOKEN_STORAGE_KEY } from '../lib/token'
import { createAppRouter } from '../routes'
import { mount } from '../test/harness'
import { installLocalStorage } from '../test/storage'

// jsdom 没有布局；轮播的拖拽在浏览器里验收
vi.mock('embla-carousel-react', () => ({ default: () => [() => {}, undefined] }))

const FRIEREN = { anilistId: 154587, title: '葬送的芙莉莲', genres: ['Adventure'], episodes: 28, format: 'TV', status: 'FINISHED' }

function stubFetch(): ReturnType<typeof vi.fn> {
  const mock = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, 'http://127.0.0.1')
    const data =
      url.pathname === '/api/catalog/search'
        ? { query: url.searchParams.get('q'), items: [FRIEREN] }
        : url.pathname === '/api/discover'
          ? { sections: [{ key: 'trending', title: 'animego 上在看最多', items: [FRIEREN] }], fetchedAt: 1 }
          : undefined
    if (data === undefined) {
      return new Response(JSON.stringify({ success: false, data: null, error: `未打桩 ${url.pathname}` }), { status: 404 })
    }
    return new Response(JSON.stringify({ success: true, data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

function searchCalls(mock: ReturnType<typeof vi.fn>): string[] {
  return mock.mock.calls
    .map(([input]) => new URL(String(input), 'http://127.0.0.1'))
    .filter((url) => url.pathname === '/api/catalog/search')
    .map((url) => url.searchParams.get('q') ?? '')
}

async function settle(): Promise<void> {
  for (let i = 0; i < 8; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
}

/** 发现页是懒加载路由：先等路由把它载完，再让页面里的请求与渲染落地 */
async function mountAt(entry: string) {
  const router = createAppRouter(createMemoryHistory({ initialEntries: [entry] }))
  const view = await mount(<RouterProvider router={router} />)
  await act(async () => { await router.load() })
  await settle()
  return { router, ...view }
}

/** 校验之后的 search（location.search 保留的是地址栏原样） */
function validated(router: ReturnType<typeof createAppRouter>): Record<string, unknown> {
  return { ...(router.state.matches[router.state.matches.length - 1]?.search as Record<string, unknown> | undefined) }
}

beforeEach(() => {
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '9f86d081884c7d659a2feaa0c55ad015')
  vi.stubGlobal('scrollTo', vi.fn())
  installLocalStorage()
})

afterEach(() => {
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

describe('发现页的作品搜索（路由一层）', () => {
  it('地址栏带 q 就搜这个词，只显示结果；顶栏「发现」仍是当前页', async () => {
    const mock = stubFetch()
    const { container, unmount } = await mountAt('/discover?q=芙莉莲')

    expect(searchCalls(mock)).toContain('芙莉莲')
    expect(container.textContent).toContain('搜索「芙莉莲」')
    expect(container.textContent).not.toContain('animego 上在看最多')
    const nav = [...container.querySelectorAll('.app-top-links a')].find((a) => a.textContent === '发现')
    expect(nav?.getAttribute('aria-current')).toBe('page')
    await unmount()
  })

  it('提交把关键词写进地址栏；「清除」回到榜单', async () => {
    const mock = stubFetch()
    const { router, container, unmount } = await mountAt('/discover')

    const input = container.querySelector<HTMLInputElement>('input[type="search"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '  芙莉莲 ')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      container.querySelector<HTMLButtonElement>('.catalog-search button[type="submit"]')!.click()
    })
    await settle()
    expect(validated(router)).toMatchObject({ q: '芙莉莲' })
    expect(searchCalls(mock)).toContain('芙莉莲')

    const clear = [...container.querySelectorAll('button')].find((b) => b.textContent === '清除')!
    await act(async () => clear.click())
    await settle()
    expect(validated(router)).not.toHaveProperty('q')
    expect(container.textContent).toContain('animego 上在看最多')
    await unmount()
  })

  it('空白关键词等于没搜（不发请求、照常显示榜单）；从作品页类型标签带来的 genre 照样保留', async () => {
    const mock = stubFetch()
    const { router, container, unmount } = await mountAt('/discover?q=%20%20&genre=Sci-Fi')

    expect(searchCalls(mock)).toEqual([])
    expect(container.textContent).toContain('animego 上在看最多')
    expect(validated(router).genre).toBe('Sci-Fi')
    await unmount()
  })
})
