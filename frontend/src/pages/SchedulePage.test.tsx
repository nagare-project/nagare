// @vitest-environment jsdom
import { act } from 'react'
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '../routes'
import { mount } from '../test/harness'
import { getAllByRole, getByRole } from '../test/queries'
import { installLocalStorage } from '../test/storage'
import { TOKEN_STORAGE_KEY } from '../lib/token'

beforeEach(() => {
  installLocalStorage()
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '00000000000000000000000000000000')
  vi.stubGlobal('scrollTo', vi.fn())
  const nowSec = Math.floor(Date.now() / 1000)
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), 'http://localhost')
      const data: unknown =
        {
          '/api/schedule': {
            fetchedAt: nowSec,
            airings: [
              {
                scheduleId: 1,
                anilistId: 154587,
                episode: 8,
                airingAt: nowSec + 1800,
                title: '葬送的芙莉莲',
                hue: 225,
                score: 91,
                genres: ['Adventure', 'Fantasy'],
                episodes: 28,
                format: 'TV',
                inLibrary: true,
              },
              {
                scheduleId: 2,
                anilistId: 185407,
                episode: 3,
                airingAt: nowSec + 86400 + 3600,
                title: '明日新番示例',
                hue: 35,
                score: 84,
                genres: ['Action'],
                episodes: 12,
                format: 'TV',
                inLibrary: false,
              },
            ],
          },
          '/api/lists': {
            loggedIn: true,
            entries: [
              {
                anilistId: 154587,
                status: 'watching',
                currentEpisode: 7,
                score: 9,
                media: { anilistId: 154587, title: '葬送的芙莉莲', episodes: 28, genres: [] },
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

describe('SchedulePage', () => {
  it('展示 7 日标签、时间线、库中徽标、本周追番与切日交互', async () => {
    const router = createAppRouter(createMemoryHistory({ initialEntries: ['/schedule'] }))
    await router.load()
    const { container, unmount } = await mount(<RouterProvider router={router} />)
    await act(async () => {})
    try {
      expect(getByRole(container, 'heading', { level: 1, name: '放送表' })).toBeDefined()
      const tabs = getAllByRole(container, 'tab')
      expect(tabs).toHaveLength(7)
      expect(tabs[0].getAttribute('aria-selected')).toBe('true')
      expect(container.textContent).toContain('葬送的芙莉莲')
      expect(container.textContent).toContain('库中')
      expect(container.textContent).toContain('我追的 · 本周')

      // 切到明天
      await act(async () => {
        tabs[1].click()
      })
      expect(tabs[1].getAttribute('aria-selected')).toBe('true')
      expect(container.textContent).toContain('明日新番示例')
    } finally {
      await unmount()
    }
  })
})
