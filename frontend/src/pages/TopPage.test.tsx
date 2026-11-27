// @vitest-environment jsdom
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '../routes'
import { mount } from '../test/harness'
import { installLocalStorage } from '../test/storage'

const topFixture = {
  items: [
    {
      anilistId: 5114,
      title: '钢之炼金术师 FA',
      cover: '/art/cap/remote/5114',
      hue: 32,
      year: 2009,
      season: '春',
      episodes: 64,
      score: 91,
      rank: 1,
      format: 'TV',
      status: 'FINISHED',
      studios: ['BONES'],
      genres: ['Action', 'Adventure'],
      description: '等价交换的炼金术之旅。',
    },
  ],
  fetchedAt: 1794000000,
}

const yearsFixture = {
  years: [{ year: 2026, count: 120 }, { year: 2025, count: 310 }],
  fetchedAt: 1794000000,
}

beforeEach(() => {
  installLocalStorage()
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.startsWith('/api/top/years')) return new Response(JSON.stringify({ success: true, data: yearsFixture }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    if (url.startsWith('/api/top')) return new Response(JSON.stringify({ success: true, data: topFixture }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    if (url.startsWith('/api/lists')) return new Response(JSON.stringify({ success: true, data: { loggedIn: false, entries: [] } }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    return new Response(JSON.stringify({ success: true, data: {} }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function settle(): Promise<void> {
  for (let i = 0; i < 6; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
}

describe('排行榜页 /top', () => {
  it('展示全站 Top 100 排名、年份条与三视图切换', async () => {
    const router = createAppRouter(createMemoryHistory({ initialEntries: ['/top'] }))
    await router.load()
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    await settle()

    expect(container.querySelector('h1')?.textContent).toBe('全站 Top 100')
    expect(container.textContent).toContain('钢之炼金术师 FA')
    expect(container.textContent).toContain('#1')
    expect(container.textContent).toContain('2026')

    const gridBtn = container.querySelector<HTMLButtonElement>('button[aria-label="封面网格"]')!
    await act(async () => gridBtn.click())
    await settle()
    expect(router.state.location.search).toMatchObject({ view: 'grid' })

    const yearBtn = [...container.querySelectorAll('button')].find((b) => b.textContent === '2025')!
    await act(async () => yearBtn.click())
    await settle()
    expect(router.state.location.search).toMatchObject({ year: 2025 })
    expect(container.querySelector('h1')?.textContent).toBe('2025 年度 Top 100')

    await unmount()
  })
})
