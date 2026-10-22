import { afterEach, describe, expect, it, vi } from 'vitest'
import { DESKTOP_UA_MARKER, inDesktopWindow, isDesktopWindowUA } from './desktopWindow'

// macOS 14 上 WKWebView 的默认 User-Agent（桌面窗口在它后面追加记号）
const WKWEBVIEW_UA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko)'
const SAFARI_UA = `${WKWEBVIEW_UA} Version/17.5 Safari/605.1.15`

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('isDesktopWindowUA', () => {
  it('认得宿主追加的记号（带不带版本都行）', () => {
    expect(isDesktopWindowUA(`${WKWEBVIEW_UA} ${DESKTOP_UA_MARKER}`)).toBe(true)
    expect(isDesktopWindowUA(`${SAFARI_UA} ${DESKTOP_UA_MARKER}`)).toBe(true)
    expect(isDesktopWindowUA(`${WKWEBVIEW_UA} ${DESKTOP_UA_MARKER}/0.7.0`)).toBe(true)
    expect(isDesktopWindowUA(DESKTOP_UA_MARKER)).toBe(true)
  })

  it('普通浏览器、前缀撞车都不算', () => {
    expect(isDesktopWindowUA(SAFARI_UA)).toBe(false)
    expect(isDesktopWindowUA(WKWEBVIEW_UA)).toBe(false)
    expect(isDesktopWindowUA(`${SAFARI_UA} NagareDesktopX`)).toBe(false)
    expect(isDesktopWindowUA(`${SAFARI_UA} xNagareDesktop`)).toBe(false)
    expect(isDesktopWindowUA('')).toBe(false)
  })
})

describe('inDesktopWindow', () => {
  it('读 navigator.userAgent', () => {
    vi.stubGlobal('navigator', { userAgent: `${WKWEBVIEW_UA} ${DESKTOP_UA_MARKER}` })
    expect(inDesktopWindow()).toBe(true)
    vi.stubGlobal('navigator', { userAgent: SAFARI_UA })
    expect(inDesktopWindow()).toBe(false)
  })

  it('没有 navigator（或 userAgent 不是字符串）时当作外部浏览器', () => {
    vi.stubGlobal('navigator', undefined)
    expect(inDesktopWindow()).toBe(false)
    vi.stubGlobal('navigator', {})
    expect(inDesktopWindow()).toBe(false)
  })
})
