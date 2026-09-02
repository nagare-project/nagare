import { describe, expect, it } from 'vitest'
import {
  errorText,
  formatBytes,
  formatClock,
  formatDate,
  formatDateTime,
  formatDuration,
  formatEpisode,
  formatVersion,
  progressPercent,
} from './format'

describe('formatDuration', () => {
  it.each<[number, string]>([
    [0, '00:00'],
    [59, '00:59'],
    [60, '01:00'],
    [61, '01:01'],
    [599, '09:59'],
    [600, '10:00'],
    [3599, '59:59'],
    [3600, '1:00:00'],
    [3661, '1:01:01'],
    [7325, '2:02:05'],
    [36000, '10:00:00'],
  ])('%s 秒 → %s', (input, expected) => {
    expect(formatDuration(input)).toBe(expected)
  })

  it.each<[number, string]>([
    [59.9, '00:59'], // 向下取整，不进位
    [-5, '00:00'],
    [NaN, '00:00'],
    [Infinity, '00:00'],
    [-Infinity, '00:00'],
  ])('异常/小数输入 %s → %s', (input, expected) => {
    expect(formatDuration(input)).toBe(expected)
  })
})

describe('formatBytes', () => {
  it.each<[number, string]>([
    [0, '0 B'],
    [1, '1 B'],
    [512, '512 B'],
    [1023, '1023 B'],
    [1024, '1 KB'],
    [1536, '1.5 KB'],
    [10 * 1024, '10 KB'],
    [1048576, '1 MB'],
    [5505024, '5.25 MB'], // 5.25 * 1024^2
    [1073741824, '1 GB'],
    [1610612736, '1.5 GB'], // 1.5 * 1024^3
    [132070244352, '123 GB'], // 123 * 1024^3 → >=100 取整
    [1099511627776, '1 TB'],
  ])('%s 字节 → %s', (input, expected) => {
    expect(formatBytes(input)).toBe(expected)
  })

  it.each<[number, string]>([
    [-1, '0 B'],
    [NaN, '0 B'],
    [Infinity, '0 B'],
  ])('非法输入 %s → %s', (input, expected) => {
    expect(formatBytes(input)).toBe(expected)
  })
})

describe('progressPercent', () => {
  it.each<[number, number, number]>([
    [0, 100, 0],
    [30, 60, 50],
    [59, 60, 98],
    [60, 60, 100],
    [120, 60, 100], // 超出封顶
    [-5, 60, 0], // 负数下限
    [10, 0, 0], // 除零
    [10, -1, 0],
    [NaN, 60, 0],
    [10, NaN, 0],
  ])('(%s, %s) → %s%%', (position, duration, expected) => {
    expect(progressPercent(position, duration)).toBe(expected)
  })
})

describe('formatEpisode', () => {
  it.each<[number, string]>([
    [0, '00'],
    [3, '03'],
    [12, '12'],
    [100, '100'],
    [3.5, '3.5'], // 半集保留小数
    [-1, '-1'], // 异常值原样透出，不硬补零
  ])('%s → %s', (input, expected) => {
    expect(formatEpisode(input)).toBe(expected)
  })
})

describe('formatClock / formatDate', () => {
  // 用本地时区构造时间戳，测试在任何时区跑都稳定
  const morning = new Date(2026, 7, 30, 9, 5, 42)

  it('毫秒时间戳 → HH:MM', () => {
    expect(formatClock(morning.getTime())).toBe('09:05')
  })

  it('秒级时间戳（Go time.Unix()）同样正确', () => {
    expect(formatClock(Math.floor(morning.getTime() / 1000))).toBe('09:05')
  })

  it('毫秒时间戳 → YYYY-MM-DD（月/日补零）', () => {
    expect(formatDate(new Date(2026, 0, 2).getTime())).toBe('2026-01-02')
  })

  it('秒级时间戳 → YYYY-MM-DD', () => {
    expect(formatDate(Math.floor(new Date(2026, 11, 31).getTime() / 1000))).toBe('2026-12-31')
  })

  it('formatDateTime → YYYY-MM-DD HH:MM（秒级与毫秒级都行）', () => {
    expect(formatDateTime(morning.getTime())).toBe('2026-08-30 09:05')
    expect(formatDateTime(Math.floor(morning.getTime() / 1000))).toBe('2026-08-30 09:05')
  })
})

describe('formatVersion', () => {
  it.each<[string, string]>([
    ['0.2.0', 'v0.2.0'],
    ['v0.2.0', 'v0.2.0'],
    ['V0.2.0', 'v0.2.0'],
    ['  v1.0.0-rc.1 ', 'v1.0.0-rc.1'],
    ['', '—'],
    ['v', '—'],
  ])('%j → %s', (input, expected) => {
    expect(formatVersion(input)).toBe(expected)
  })
})

describe('errorText', () => {
  it('Error 的 message 直接透出（信封中文错误走这条路）', () => {
    expect(errorText(new Error('路径不存在：/tmp/nope'), '兜底')).toBe('路径不存在：/tmp/nope')
  })

  it('空 message 回退到兜底文案', () => {
    expect(errorText(new Error(''), '兜底')).toBe('兜底')
  })

  it('非 Error 值一律回退', () => {
    expect(errorText('boom', '兜底')).toBe('兜底')
    expect(errorText(undefined, '兜底')).toBe('兜底')
    expect(errorText({ message: '假的' }, '兜底')).toBe('兜底')
  })
})
