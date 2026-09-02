// @vitest-environment jsdom
// M2 契约层：只验「路径 + 编码 + 方法 + body」，信封解析归 api.ts 的测试管。
// apiFetch 会读 sessionStorage 里的 token，所以需要 jsdom。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  fetchSources,
  reloadSources,
  searchMagnets,
  selfCheckSource,
  setSourceEnabled,
  syncSources,
  updateRulesConfig,
} from './endpoints'
import { TOKEN_STORAGE_KEY } from './token'

interface Captured {
  url: string
  method: string
  body: unknown
}

/** 把 fetch 桩成「记录请求、回一个空 data 信封」 */
function stubFetch(data: unknown = {}): { calls: Captured[] } {
  const calls: Captured[] = []
  const impl = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const rawBody = init?.body
    calls.push({
      url,
      method: init?.method ?? 'GET',
      body: typeof rawBody === 'string' ? (JSON.parse(rawBody) as unknown) : undefined,
    })
    return new Response(JSON.stringify({ success: true, data }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  vi.stubGlobal('fetch', vi.fn(impl))
  return { calls }
}

beforeEach(() => {
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '9f86d081884c7d659a2feaa0c55ad015')
})

afterEach(() => {
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

describe('searchMagnets', () => {
  it('q 走 encodeURIComponent：中文与 & 都不能裸露在 query 里', async () => {
    const { calls } = stubFetch({ query: '', items: [], sources: [] })
    const query = '葬送的芙莉莲 & 1080p #2'
    await searchMagnets(query)

    expect(calls).toHaveLength(1)
    const { url, method } = calls[0]
    expect(method).toBe('GET')
    expect(url).toBe(`/api/search?q=${encodeURIComponent(query)}`)
    // 编码后只有一个 query 参数，& 与 # 没有把它切开
    expect(url.split('?')[1]).not.toContain('&')
    expect(url).not.toContain('#')
    // 反解回来与原文一致
    expect(new URL(url, 'http://127.0.0.1').searchParams.get('q')).toBe(query)
  })

  it('返回信封里的 data', async () => {
    const result = { query: 'x', items: [], sources: [] }
    stubFetch(result)
    await expect(searchMagnets('x')).resolves.toEqual(result)
  })
})

describe('sources 端点', () => {
  it('fetchSources → GET /api/sources', async () => {
    const { calls } = stubFetch({ sources: [], rules: {} })
    await fetchSources()
    expect(calls[0]).toMatchObject({ url: '/api/sources', method: 'GET' })
  })

  it('setSourceEnabled：id 进路径要编码，body 只带 enabled', async () => {
    const { calls } = stubFetch()
    await setSourceEnabled('a/b c', false)
    expect(calls[0]).toEqual({
      url: '/api/sources/a%2Fb%20c/enabled',
      method: 'POST',
      body: { enabled: false },
    })
  })

  it('selfCheckSource → POST /api/sources/{id}/selfcheck，无 body', async () => {
    const outcome = { source: 'x', state: 'ok', count: 1, rawCount: 1, dropped: 0, latencyMs: 5 }
    const { calls } = stubFetch(outcome)
    await expect(selfCheckSource('x')).resolves.toEqual(outcome)
    expect(calls[0]).toEqual({ url: '/api/sources/x/selfcheck', method: 'POST', body: undefined })
  })

  it('reloadSources / syncSources → POST 固定路径', async () => {
    const { calls } = stubFetch({ loaded: 0, errors: [] })
    await reloadSources()
    await syncSources()
    expect(calls.map((call) => [call.url, call.method])).toEqual([
      ['/api/sources/reload', 'POST'],
      ['/api/sources/sync', 'POST'],
    ])
  })

  it('updateRulesConfig：body 只带传入的字段', async () => {
    const { calls } = stubFetch({ remoteUrl: 'https://example.invalid/rules' })
    await updateRulesConfig({ remoteUrl: 'https://example.invalid/rules' })
    expect(calls[0]).toEqual({
      url: '/api/sources/config',
      method: 'POST',
      body: { remoteUrl: 'https://example.invalid/rules' },
    })
  })
})
