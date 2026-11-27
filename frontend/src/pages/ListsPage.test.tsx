// @vitest-environment jsdom
import { act } from 'react'
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '../routes'
import { mount } from '../test/harness'
import { getAllByRole, getByRole } from '../test/queries'
import { installLocalStorage } from '../test/storage'
import { TOKEN_STORAGE_KEY } from '../lib/token'

let savedBody: Record<string, unknown> | null = null

beforeEach(() => {
  savedBody = null
  installLocalStorage()
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '00000000000000000000000000000000')
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input), 'http://localhost')
      if (url.pathname === '/api/lists/154587' && init?.method === 'POST') {
        savedBody = JSON.parse(String(init.body)) as Record<string, unknown>
        return new Response(
          JSON.stringify({
            success: true,
            data: {
              anilistId: 154587,
              status: 'watching',
              currentEpisode: 8,
              score: 9,
              media: { anilistId: 154587, title: '葬送的芙莉莲', episodes: 28, genres: ['Fantasy'], hue: 225 },
            },
          }),
          { headers: { 'Content-Type': 'application/json' } },
        )
      }
      const data: unknown =
        {
          '/api/lists': {
            loggedIn: true,
            entries: [
              {
                anilistId: 154587,
                status: 'watching',
                currentEpisode: 7,
                score: 9,
                media: { anilistId: 154587, title: '葬送的芙莉莲', episodes: 28, genres: ['Fantasy'], hue: 225 },
              },
              {
                anilistId: 185407,
                status: 'completed',
                currentEpisode: 12,
                score: 10,
                media: { anilistId: 185407, title: '完结神作', episodes: 12, genres: ['Drama'], hue: 40 },
              },
            ],
          },
          '/api/library': { folders: [], clusters: [], continueWatching: [], scannedAt: 1 },
        }[url.pathname] ?? {}
      return new Response(JSON.stringify({ success: true, data }), {
        headers: { 'Content-Type': 'application/json' },
      })
    }),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

describe('ListsPage', () => {
  it('展示在看横向特性卡片、支持 +1 集快捷记录进度与状态标签切换', async () => {
    const router = createAppRouter(createMemoryHistory({ initialEntries: ['/lists'] }))
    await router.load()
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    await act(async () => {})
    try {
      expect(getByRole(container, 'heading', { level: 1, name: '我的追番' })).toBeDefined()
      expect(container.textContent).toContain('葬送的芙莉莲')
      expect(container.textContent).toContain('看到第 7 集')
      expect(container.textContent).toContain('7/28 集')

      const stepBtn = getByRole(container, 'button', { name: '「葬送的芙莉莲」进度 +1 集' })
      await act(async () => { stepBtn.click() })
      expect(savedBody).toEqual({ status: 'watching', currentEpisode: 8, score: 9 })

      const tabs = getAllByRole(container, 'tab')
      await act(async () => { tabs[2].click() })
      expect(container.textContent).toContain('完结神作')
    } finally {
      await unmount()
    }
  })
})
