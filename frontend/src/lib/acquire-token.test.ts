// @vitest-environment jsdom
// acquireToken 直接操作 window.location / sessionStorage / history，
// 只给这个文件启用 jsdom，其余测试保持 node 环境的零开销。
import { beforeEach, describe, expect, it } from 'vitest'
import { acquireToken, TOKEN_STORAGE_KEY } from './token'

const HEX_TOKEN = '9f86d081884c7d659a2feaa0c55ad015'
const OTHER_TOKEN = '0123456789abcdef0123456789abcdef'

// 用 replaceState 摆好地址栏，再断言 acquireToken 的完整副作用链。
function setUrl(pathAndQuery: string, state: unknown = null): void {
  window.history.replaceState(state, '', pathAndQuery)
}

beforeEach(() => {
  window.sessionStorage.clear()
  setUrl('/')
})

describe('acquireToken', () => {
  it('URL 带合法 token：入库、返回、并从地址栏抹掉', () => {
    setUrl(`/?token=${HEX_TOKEN}`)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
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

  it('URL 没有 token 时回读 sessionStorage', () => {
    window.sessionStorage.setItem(TOKEN_STORAGE_KEY, HEX_TOKEN)
    expect(acquireToken()).toBe(HEX_TOKEN)
  })

  it('URL token 优先于 sessionStorage 里的旧值，并覆盖入库', () => {
    window.sessionStorage.setItem(TOKEN_STORAGE_KEY, OTHER_TOKEN)
    setUrl(`/?token=${HEX_TOKEN}`)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(HEX_TOKEN)
  })

  it('URL 里是垃圾值（CRLF 注入试探）：不入库、返回 null、仍从地址栏抹掉', () => {
    setUrl('/?token=abc%0D%0Adef')
    expect(acquireToken()).toBe(null)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(null)
    expect(window.location.search).toBe('')
  })

  it('sessionStorage 里的坏值会被清掉并视作没有 token', () => {
    window.sessionStorage.setItem(TOKEN_STORAGE_KEY, 'not-a-token')
    expect(acquireToken()).toBe(null)
    expect(window.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe(null)
  })

  it('两处都没有时返回 null', () => {
    expect(acquireToken()).toBe(null)
  })

  it('重复调用幂等：抹掉地址栏后第二次从 sessionStorage 拿到同一个值', () => {
    setUrl(`/?token=${HEX_TOKEN}`)
    expect(acquireToken()).toBe(HEX_TOKEN)
    expect(acquireToken()).toBe(HEX_TOKEN)
  })
})
