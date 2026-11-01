import { describe, expect, it } from 'vitest'
import { freshnessNotable, freshnessText, relativeTime, shouldRefetchStale } from './freshness'
import { posterToneStyle } from './posterTones'

const NOW = Date.parse('2026-11-01T12:00:00Z')
const ago = (ms: number) => new Date(NOW - ms).toISOString()

describe('「更新于 …」的相对时间', () => {
  it('刚刚 / 分钟 / 小时 / 天，再早给日期；时钟回拨按刚刚', () => {
    expect(relativeTime(ago(30_000), NOW)).toBe('刚刚')
    expect(relativeTime(ago(-60_000), NOW)).toBe('刚刚')
    expect(relativeTime(ago(5 * 60_000), NOW)).toBe('5 分钟前')
    expect(relativeTime(ago(2 * 3600_000 + 59 * 60_000), NOW)).toBe('2 小时前')
    expect(relativeTime(ago(3 * 86400_000), NOW)).toBe('3 天前')
    expect(relativeTime(ago(40 * 86400_000), NOW)).toMatch(/^2026-09-2\d$/)
  })

  it('解析不了的时间返回 null，文案换成不带时间的说法', () => {
    expect(relativeTime('不是时间', NOW)).toBeNull()
    expect(freshnessText({ fetchedAt: '坏', stale: false, offline: true }, NOW)).toBe('离线 · 显示的是上次保存的内容')
  })
})

describe('数据来历', () => {
  it('离线或过期才提；只是来自盘上、还在时限内的不提', () => {
    expect(freshnessNotable(undefined)).toBe(false)
    expect(freshnessNotable({ fetchedAt: ago(0), stale: false, offline: false })).toBe(false)
    expect(freshnessNotable({ fetchedAt: ago(0), stale: true, offline: false })).toBe(true)
    expect(freshnessNotable({ fetchedAt: ago(0), stale: false, offline: true })).toBe(true)
  })

  it('离线说「离线」；只是过期不说离线，等后台那次重取时说正在刷新', () => {
    const at = ago(2 * 3600_000)
    expect(freshnessText({ fetchedAt: at, stale: true, offline: true }, NOW)).toBe('离线 · 更新于 2 小时前')
    expect(freshnessText({ fetchedAt: at, stale: true, offline: false }, NOW, true)).toBe('更新于 2 小时前 · 正在刷新')
    expect(freshnessText({ fetchedAt: at, stale: true, offline: false }, NOW, false)).toBe('更新于 2 小时前')
  })

  it('只在过期且在线时后台再取一次', () => {
    expect(shouldRefetchStale({ fetchedAt: ago(0), stale: true, offline: false })).toBe(true)
    expect(shouldRefetchStale({ fetchedAt: ago(0), stale: true, offline: false }, false)).toBe(false)
    expect(shouldRefetchStale({ fetchedAt: ago(0), stale: true, offline: true })).toBe(false)
    expect(shouldRefetchStale({ fetchedAt: ago(0), stale: false, offline: false })).toBe(false)
    expect(shouldRefetchStale(undefined)).toBe(false)
  })
})

describe('海报色阶 → 行内样式', () => {
  const tones = { hue: 250, tone: '#88b6e6', quiet: '#779cc2', mid: '#496c90', low: '#0d2034', onTone: '#030c17' }

  it('五档都写成 --poster-tone* 自定义属性', () => {
    expect(posterToneStyle(tones)).toEqual({
      '--poster-tone': '#88b6e6', '--poster-tone-quiet': '#779cc2', '--poster-tone-mid': '#496c90',
      '--poster-tone-low': '#0d2034', '--poster-on-tone': '#030c17',
    })
  })

  it('没有色阶、或任何一档不是 #rrggbb 时不写（落回 tokens.css 的中性值）', () => {
    expect(posterToneStyle(null)).toBeUndefined()
    expect(posterToneStyle(undefined)).toBeUndefined()
    expect(posterToneStyle({ ...tones, mid: 'url(https://example.com/x)' })).toBeUndefined()
    expect(posterToneStyle({ ...tones, tone: 'var(--accent)' })).toBeUndefined()
  })
})
