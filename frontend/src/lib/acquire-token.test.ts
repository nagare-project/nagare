// @vitest-environment jsdom
// acquireToken 直接操作 window.location / 浏览器存储 / history，
// 只给这个文件启用 jsdom，其余测试保持 node 环境的零开销。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TOKEN_STORAGE_KEY } from './token'
import { installLocalStorage } from '../test/storage'

let acquireToken: typeof import('./token').acquireToken

const HEX_TOKEN = '9f86d081884c7d659a2feaa0c55ad015'
const OTHER_TOKEN = '0123456789abcdef0123456789abcdef'

// 用 replaceState 摆好地址栏，再断言 acquireToken 的完整副作用链。
function setUrl(pathAndQuery: string, state: unknown = null): void {
  window.history.replaceState(state, '', pathAndQuery)
}

beforeEach(async () => {
  vi.resetModules()
  acquireToken = (await import('./token')).acquireToken
  installLocalStorage()
  window.sessionStorage.clear()
  setUrl('/')
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('acquireToken', () => {
  it('URL 带合法 token：入库、返回、并从地址栏抹掉', () => {
    setUrl(`/?token=${HEX_TOKEN}`)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
    expect(window.location.search).not.toContain('token')
  })

  it('抹除 token 时保留其余 query 参数、hash 与 history state', () => {
    const state = { from: 'test' }
    setUrl(`/library?foo=1&token=${HEX_TOKEN}&bar=2#section`, state)
    acquireToken()
    expect(window.location.pathname).toBe('/library')
    expect(window.location.search).toBe('?foo=1&bar=2')
    expect(window.location.hash).toBe('#section')
    expect(window.history.state).toEqual(state)
  })

  it('URL 没有 token 时迁移旧版 sessionStorage', () => {
    window.sessionStorage.setItem(TOKEN_STORAGE_KEY, HEX_TOKEN)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
  })

  it('URL token 优先于两种存储里的旧值，并覆盖入库', () => {
    window.sessionStorage.setItem(TOKEN_STORAGE_KEY, OTHER_TOKEN)
    window.localStorage.setItem(TOKEN_STORAGE_KEY, OTHER_TOKEN)
    setUrl(`/?token=${HEX_TOKEN}`)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
  })

  it('URL 里是垃圾值（CRLF 注入试探）：不入库、返回 null、仍从地址栏抹掉', () => {
    setUrl('/?token=abc%0D%0Adef')
    expect(acquireToken()).toBe(null)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(null)
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBe(null)
    expect(window.location.search).toBe('')
  })

  it('sessionStorage 里的坏值会被清掉并视作没有 token', () => {
    window.sessionStorage.setItem(TOKEN_STORAGE_KEY, 'not-a-token')
    expect(acquireToken()).toBe(null)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(null)
  })

  it('没有凭证时返回 null', () => {
    expect(acquireToken()).toBe(null)
  })

  it('重复调用幂等：抹掉地址栏后仍拿到同一个值', () => {
    setUrl(`/?token=${HEX_TOKEN}`)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(acquireToken()).toBe(HEX_TOKEN)
  })

  it('新标签页没有 sessionStorage 和内存状态时，API 仍带上持久凭证', async () => {
    setUrl(`/discover?token=${HEX_TOKEN}`)
    acquireToken()
    window.sessionStorage.clear()
    vi.resetModules()
    const { apiFetch, TOKEN_HEADER } = await import('./api')
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({ success: true, data: { status: 'ok' } })))
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiFetch('/api/health')).resolves.toEqual({ status: 'ok' })
    const [, init] = fetchMock.mock.calls[0]!
    expect(new Headers(init?.headers).get(TOKEN_HEADER)).toBe(HEX_TOKEN)
    expect(window.location.pathname).toBe('/discover')
    expect(window.location.search).toBe('')
  })

  it('另一标签页更新了持久凭证后，旧标签页不会继续使用旧会话值', () => {
    window.sessionStorage.setItem(TOKEN_STORAGE_KEY, OTHER_TOKEN)
    window.localStorage.setItem(TOKEN_STORAGE_KEY, HEX_TOKEN)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
  })

  it('非法 URL 不会覆盖已保存的有效凭证', () => {
    window.localStorage.setItem(TOKEN_STORAGE_KEY, HEX_TOKEN)
    setUrl('/discover?token=garbage')
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
    expect(window.location.search).toBe('')
  })

  it('清理持久存储中的垃圾值', () => {
    window.localStorage.setItem(TOKEN_STORAGE_KEY, 'not-a-token')
    expect(acquireToken()).toBeNull()
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('持久存储被禁用时仍可使用当前标签页', () => {
    vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => { throw new DOMException('blocked', 'SecurityError') })
    setUrl(`/?token=${HEX_TOKEN}`)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
  })

  it('两种存储均不可写时，抹掉启动 URL 后仍使用新凭证', () => {
    window.localStorage.setItem(TOKEN_STORAGE_KEY, OTHER_TOKEN)
    window.sessionStorage.setItem(TOKEN_STORAGE_KEY, OTHER_TOKEN)
    vi.spyOn(window.localStorage, 'setItem').mockImplementation(() => { throw new DOMException('full', 'QuotaExceededError') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('full', 'QuotaExceededError') })
    setUrl(`/?token=${HEX_TOKEN}`)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(window.location.search).toBe('')
    expect(acquireToken()).toBe(HEX_TOKEN)
  })

  it('持久存储写满时，不会用未覆盖的旧值替换新启动凭证', () => {
    window.localStorage.setItem(TOKEN_STORAGE_KEY, OTHER_TOKEN)
    vi.spyOn(window.localStorage, 'setItem').mockImplementation(() => { throw new DOMException('full', 'QuotaExceededError') })
    setUrl(`/?token=${HEX_TOKEN}`)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
    expect(acquireToken()).toBe(HEX_TOKEN)
  })
})
