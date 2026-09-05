import { describe, expect, it } from 'vitest'
import { calendarDays, localDateKey, shiftMonth } from './calendar'
import type { ScheduleEvent } from './types'

describe('本地月历日期', () => {
  it('31 日翻月不跳月，跨年和闰年都按整周补齐', () => {
    expect(localDateKey(shiftMonth(new Date(2026, 0, 31), 1))).toBe('2026-02-01')
    expect(localDateKey(shiftMonth(new Date(2026, 11, 31), 1))).toBe('2027-01-01')
    const february = calendarDays(new Date(2024, 1, 1), 1, [], new Date(2024, 1, 29))
    expect(february.filter(d => d.currentMonth)).toHaveLength(29)
    expect(february.filter(d => d.today).map(d => d.key)).toEqual(['2024-02-29'])
    const march = calendarDays(new Date(2026, 2, 1), 1, [], new Date(2026, 2, 1))
    expect(march).toHaveLength(42)
    expect(march[0]?.key).toBe('2026-02-23')
    expect(march.at(-1)?.key).toBe('2026-04-05')
    expect(new Set(march.map(d => d.key)).size).toBe(42)
  })
  it('切换周首不改变实际日期或事件，午夜 ISO 时间按本地日期归组', () => {
    const early = new Date(2026, 8, 4, 0, 15)
    const events = [{ id: 'later', airingAt: new Date(2026, 8, 4, 22).toISOString(), episode: 2 },
      { id: 'early', airingAt: early.toISOString(), episode: 1 }] as ScheduleEvent[]
    for (const startsOn of [0, 1] as const) {
      const days = calendarDays(early, startsOn, events, early)
      expect(days[0]?.date.getDay()).toBe(startsOn)
      expect(days.filter(d => d.events.length).map(d => d.key)).toEqual(['2026-09-04'])
      expect(days.find(d => d.key === '2026-09-04')?.events.map(e => e.id)).toEqual(['early', 'later'])
    }
  })
})
