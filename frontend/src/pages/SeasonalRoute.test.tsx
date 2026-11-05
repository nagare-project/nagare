// @vitest-environment jsdom
import { act } from 'react'
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '../routes'
import { SEASON_LABEL, currentSeason } from '../components/catalog/season'
import { mount } from '../test/harness'
import { getByRole } from '../test/queries'
import { installLocalStorage } from '../test/storage'
import { TOKEN_STORAGE_KEY } from '../lib/token'

/** 页面请求过的季度目录（/api/seasonal 的查询参数） */
let requested: string[] = []

beforeEach(() => {
  requested = []
  installLocalStorage()
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '00000000000000000000000000000000')
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'http://localhost')
    if (url.pathname === '/api/seasonal') requested.push(url.search)
    const data: Record<string, unknown> = {
      '/api/seasonal': { items: [] },
      '/api/discover': { sections: [] },
      '/api/library': { folders: [{ id: 'f', path: '/番剧', addedAt: 1 }], clusters: [], continueWatching: [], scannedAt: 1 },
    }[url.pathname] ?? {}
    return new Response(JSON.stringify({ success: true, data }), { headers: { 'Content-Type': 'application/json' } })
  }))
})
afterEach(() => {
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

async function open(path: string) {
  const router = createAppRouter(createMemoryHistory({ initialEntries: [path] }))
  await router.load()
  const mounted = await mount(<RouterProvider router={router} />)
  await act(async () => {})
  return { ...mounted, router }
}

/** 路由校验之后交给页面的参数（最里层那个 match 的 search） */
function validated(router: ReturnType<typeof createAppRouter>): Record<string, unknown> {
  return { ...(router.state.matches.at(-1)?.search as Record<string, unknown> | undefined) }
}

describe('季度页的地址参数', () => {
  it('季度、年份、排序认不出来时落到当前季度：不显示「undefined」，也不拿坏参数去请求', async () => {
    const now = currentSeason()
    for (const query of ['?season=foo&year=abc&sort=bogus', '?season=%5B%22x%22%5D&year=1800', '?season=hasOwnProperty&year=2101']) {
      const { container, unmount } = await open(`/seasonal${query}`)
      try {
        expect(getByRole(container, 'heading', { level: 1 }).textContent).toBe(`${now.year} 年${SEASON_LABEL[now.season]}季新番`)
        expect(container.textContent).not.toContain('undefined')
        expect(container.textContent).not.toContain('NaN')
        expect((getByRole(container, 'combobox', { name: '排序' }) as HTMLSelectElement).value).toBe('score')
        expect(requested.at(-1)).toBe(`?season=${now.season}&year=${now.year}`)
      } finally {
        await unmount()
      }
    }
  })

  it('认得出的参数照常生效（季度不分大小写）', async () => {
    const { container, unmount } = await open('/seasonal?season=spring&year=2024&sort=title')
    expect(getByRole(container, 'heading', { level: 1 }).textContent).toBe('2024 年春季新番')
    expect((getByRole(container, 'combobox', { name: '排序' }) as HTMLSelectElement).value).toBe('title')
    expect(requested.at(-1)).toBe('?season=SPRING&year=2024')
    await unmount()
  })
})

describe('发现页的地址参数', () => {
  it('认不出的 q / genre（数组、数字）不会原样漏给页面：当作没带，照常显示发现页', async () => {
    const { container, router, unmount } = await open('/?q=%5B1%5D&genre=1')
    expect(validated(router).q).toBeUndefined()
    expect(validated(router).genre).toBeUndefined()
    expect(getByRole(container, 'heading', { level: 1, name: '发现' })).toBeDefined()
    expect(container.textContent).not.toContain('搜索「')
    await unmount()
  })
})
