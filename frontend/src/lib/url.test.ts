import { describe, expect, it } from 'vitest'
import { isHttpUrl } from './url'

describe('isHttpUrl', () => {
  it.each(['https://mpv.io/installation/', 'http://example.test', 'HTTPS://X'])(
    '%s → true',
    (url) => {
      expect(isHttpUrl(url)).toBe(true)
    },
  )

  it.each(['javascript:alert(1)', 'data:text/html,hi', '', 'ftp://x', ' https://x'])(
    '%s → false',
    (url) => {
      expect(isHttpUrl(url)).toBe(false)
    },
  )
})
