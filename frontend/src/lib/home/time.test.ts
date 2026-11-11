import { describe, expect, test } from 'vitest'
import { clockParts, dayHeader, hhmm, nextDayKey, parseDayKey, weekdayLabel, weekdayTime } from './time'

// 2026-10-07 12:00 UTC = 上海 20:00、东京 21:00、悉尼 23:00（夏令时）
const NOON_UTC = Date.UTC(2026, 9, 7, 12, 0)

describe('nextDayKey', () => {
  test('下一天，跨月跨年', () => {
    expect(nextDayKey('2026-10-07')).toBe('2026-10-08')
    expect(nextDayKey('2026-10-31')).toBe('2026-11-01')
    expect(nextDayKey('2026-12-31')).toBe('2027-01-01')
    expect(nextDayKey('2028-02-28')).toBe('2028-02-29')
  })

  test('不是真实日期时为 null', () => {
    expect(nextDayKey('2026-02-30')).toBeNull()
    expect(nextDayKey('今天')).toBeNull()
  })
})

describe('clockParts', () => {
  test('按给定时区读墙上的钟', () => {
    expect(clockParts(NOON_UTC, 'Asia/Shanghai')).toEqual({ dayKey: '2026-10-07', weekday: 3, hh: '20', mm: '00' })
  })

  test('午夜是 00，不是 24', () => {
    expect(clockParts(Date.UTC(2026, 9, 7, 16, 0), 'Asia/Shanghai').hh).toBe('00')
  })

  test('同一刻在另一个时区可以是另一天', () => {
    expect(clockParts(NOON_UTC, 'Asia/Shanghai').dayKey).toBe('2026-10-07')
    expect(clockParts(Date.UTC(2026, 9, 7, 14, 0), 'Australia/Sydney').dayKey).toBe('2026-10-08')
  })
})

describe('hhmm / weekdayTime', () => {
  test('24 小时制、补零', () => {
    expect(hhmm(Date.UTC(2026, 9, 7, 1, 5), 'UTC')).toBe('01:05')
  })

  test('星期用中文', () => {
    expect(weekdayTime(NOON_UTC, 'Asia/Shanghai')).toBe('周三 20:00')
  })
})

describe('dayHeader / parseDayKey / weekdayLabel', () => {
  test('「周三 10月7日」，不经过任何时区', () => {
    expect(dayHeader('2026-10-07')).toBe('周三 10月7日')
  })

  test('解析不了的给空串，不是「NaN」', () => {
    expect(dayHeader('2026-13-01')).toBe('')
  })

  test('读出日期与星期', () => {
    expect(parseDayKey('2026-10-11')).toEqual({ year: 2026, month: 10, day: 11, weekday: 0 })
    expect(parseDayKey('2026-1-1')).toBeNull()
  })

  test('周日开头；一周之外的序号给空串', () => {
    expect(weekdayLabel(0)).toBe('周日')
    expect(weekdayLabel(6)).toBe('周六')
    expect(weekdayLabel(7)).toBe('')
  })
})
