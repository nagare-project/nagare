import { describe, expect, it } from 'vitest'
import { isValidToken, parseTokenFromSearch } from './token'

// 与后端约定一致的 32 位小写 hex token 样例
const HEX_TOKEN = '9f86d081884c7d659a2feaa0c55ad015'
const OTHER_TOKEN = '0123456789abcdef0123456789abcdef'

describe('parseTokenFromSearch', () => {
  it('解析标准启动链接的 query string（带前导问号）', () => {
    expect(parseTokenFromSearch(`?token=${HEX_TOKEN}`)).toBe(HEX_TOKEN)
  })

  it('接受不带前导问号的输入', () => {
    expect(parseTokenFromSearch(`token=${HEX_TOKEN}`)).toBe(HEX_TOKEN)
  })

  it('token 混在其他参数中间时仍能取到', () => {
    expect(parseTokenFromSearch(`?foo=1&token=${HEX_TOKEN}&bar=baz`)).toBe(HEX_TOKEN)
  })

  it('对 percent-encoding 做标准解码', () => {
    const encoded = HEX_TOKEN.replace(/^9f/, '%39%66')
    expect(parseTokenFromSearch(`?token=${encoded}`)).toBe(HEX_TOKEN)
  })

  it('token 重复出现时取第一个', () => {
    expect(parseTokenFromSearch(`?token=${HEX_TOKEN}&token=${OTHER_TOKEN}`)).toBe(HEX_TOKEN)
  })

  it('没有 token 参数时返回 null', () => {
    expect(parseTokenFromSearch('?foo=1&bar=2')).toBe(null)
  })

  it('query string 为空串时返回 null', () => {
    expect(parseTokenFromSearch('')).toBe(null)
  })

  it('只有孤零零一个问号时返回 null', () => {
    expect(parseTokenFromSearch('?')).toBe(null)
  })

  it('token 为空值（?token=）时视同缺失，返回 null', () => {
    expect(parseTokenFromSearch('?token=')).toBe(null)
  })

  it('token 无值（?token）时视同缺失，返回 null', () => {
    expect(parseTokenFromSearch('?token')).toBe(null)
  })

  it('参数名大小写敏感：TOKEN 不算数', () => {
    expect(parseTokenFromSearch(`?TOKEN=${HEX_TOKEN}`)).toBe(null)
  })

  describe('形状校验：不符合 32 位小写 hex 的值一律拒绝', () => {
    it('带 CRLF 的注入试探（会让 Headers.set 抛错的那类值）', () => {
      expect(parseTokenFromSearch('?token=abc%0D%0Adef')).toBe(null)
    })

    it('长度不足', () => {
      expect(parseTokenFromSearch('?token=abc123')).toBe(null)
    })

    it('长度超出', () => {
      expect(parseTokenFromSearch(`?token=${HEX_TOKEN}ff`)).toBe(null)
    })

    it('含非 hex 字符', () => {
      expect(parseTokenFromSearch('?token=zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz')).toBe(null)
    })

    it('大写 hex 也拒绝（后端只发小写）', () => {
      expect(parseTokenFromSearch(`?token=${HEX_TOKEN.toUpperCase()}`)).toBe(null)
    })
  })
})

describe('isValidToken', () => {
  it('接受合法 token', () => {
    expect(isValidToken(HEX_TOKEN)).toBe(true)
  })

  it('拒绝 null / 空串 / 坏形状', () => {
    expect(isValidToken(null)).toBe(false)
    expect(isValidToken('')).toBe(false)
    expect(isValidToken('not-a-token')).toBe(false)
  })
})
