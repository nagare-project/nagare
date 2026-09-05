// @vitest-environment jsdom
// M2/M4 契约层：只验「路径 + 编码 + 方法 + body」，信封解析归 api.ts 的测试管。
// apiFetch 会读 sessionStorage 里的 token，所以需要 jsdom。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  applyUpdate,
  checkUpdate,
  fetchSources,
  fetchUpdate,
  redetectMpv,
  reloadSources,
  searchMagnets,
  selfCheckSource,
  setSourceEnabled,
  setUpdateEnabled,
  shutdownNagare,
  syncSources,
  updateRulesConfig,
} from './endpoints'
import { TOKEN_STORAGE_KEY } from './token'
import { installLocalStorage } from '../test/storage'

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
  installLocalStorage()
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

describe('M4 运行时端点', () => {
  it('redetectMpv → POST /api/mpv/detect，无 body，返回 MpvInfo', async () => {
    const info = { found: true, path: '/opt/homebrew/bin/mpv', version: '0.38.0', source: 'path' }
    const { calls } = stubFetch(info)
    await expect(redetectMpv()).resolves.toEqual(info)
    expect(calls[0]).toEqual({ url: '/api/mpv/detect', method: 'POST', body: undefined })
  })

  it('fetchUpdate → GET /api/update；checkUpdate → POST /api/update/check', async () => {
    const view = {
      enabled: true,
      current: '0.1.0',
      latest: '',
      available: false,
      url: '',
      checkedAt: null,
      error: '',
    }
    const { calls } = stubFetch(view)
    await expect(fetchUpdate()).resolves.toEqual(view)
    await expect(checkUpdate()).resolves.toEqual(view)
    expect(calls.map((call) => [call.url, call.method, call.body])).toEqual([
      ['/api/update', 'GET', undefined],
      ['/api/update/check', 'POST', undefined],
    ])
  })

  it('setUpdateEnabled → POST /api/update/config，body 只带 enabled', async () => {
    const { calls } = stubFetch({ enabled: false })
    await setUpdateEnabled(false)
    expect(calls[0]).toEqual({
      url: '/api/update/config',
      method: 'POST',
      body: { enabled: false },
    })
  })

  it('applyUpdate → POST /api/update/apply，无 body，返回新版本号', async () => {
    const { calls } = stubFetch({ version: '0.2.0' })
    await expect(applyUpdate()).resolves.toEqual({ version: '0.2.0' })
    expect(calls[0]).toEqual({ url: '/api/update/apply', method: 'POST', body: undefined })
  })

  it('shutdownNagare → POST /api/shutdown，空 data 也算成功', async () => {
    const { calls } = stubFetch({})
    await expect(shutdownNagare()).resolves.toBeUndefined()
    expect(calls[0]).toEqual({ url: '/api/shutdown', method: 'POST', body: undefined })
  })
})
