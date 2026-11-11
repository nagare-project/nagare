import { describe, expect, test } from 'vitest'
import { SOON_WINDOW_MS, airingsOnDay, dayAfter, slotToday } from './todaySlots'

const MIN = 60_000
const NOW = Date.UTC(2026, 9, 7, 12, 0)
const at = (key: number, offsetMin: number) => ({ key, at: NOW + offsetMin * MIN })

describe('slotToday', () => {
  test('以现在为界分成已播 / 快播 / 晚些，标记画在中间', () => {
    const { slots, nowIndex } = slotToday([at(1, -120), at(2, -30), at(3, 2), at(4, 45), at(5, 180)], NOW)
    expect(slots.map((s) => s.state)).toEqual(['aired', 'aired', 'soon', 'soon', 'later'])
    expect(nowIndex).toBe(2)
  })

  test('正好现在播出的算已播', () => {
    expect(slotToday([at(1, 0)], NOW).slots[0]!.state).toBe('aired')
  })

  test('「快播」是接下来一小时，含一小时整', () => {
    expect(slotToday([{ key: 1, at: NOW + SOON_WINDOW_MS }], NOW).slots[0]!.state).toBe('soon')
    expect(slotToday([{ key: 1, at: NOW + SOON_WINDOW_MS + 1 }], NOW).slots[0]!.state).toBe('later')
  })

  test('分钟数向上取整，还没播的不会显示「0 分钟后」', () => {
    expect(slotToday([{ key: 1, at: NOW + 1 }], NOW).slots[0]!.minutesUntil).toBe(1)
    expect(slotToday([at(1, 45)], NOW).slots[0]!.minutesUntil).toBe(45)
  })

  test('已播与晚些的不带分钟数', () => {
    const { slots } = slotToday([at(1, -5), at(2, 300)], NOW)
    expect(slots.map((s) => s.minutesUntil)).toEqual([null, null])
  })

  test('时钟走过一集的播出时间：它换状态，不离开这一排', () => {
    const items = [at(1, -10), at(2, 2), at(3, 30)]
    const before = slotToday(items, NOW)
    const after = slotToday(items, NOW + 3 * MIN)
    expect(after.slots.map((s) => s.item.key)).toEqual(before.slots.map((s) => s.item.key))
    expect(after.slots.map((s) => s.state)).toEqual(['aired', 'aired', 'soon'])
  })

  test('一集都没播时标记在最前，都播完时在最后', () => {
    expect(slotToday([at(1, 10)], NOW).nowIndex).toBe(0)
    expect(slotToday([at(1, -10)], NOW).nowIndex).toBe(1)
  })

  test('输入顺序不要紧；同一时刻保持输入顺序；不改输入', () => {
    const input = [at(3, 30), at(1, -10), at(2, 30)]
    const { slots } = slotToday(input, NOW)
    expect(slots.map((s) => s.item.key)).toEqual([1, 3, 2])
    expect(input.map((i) => i.key)).toEqual([3, 1, 2])
  })
})

describe('airingsOnDay / dayAfter：「今天」按本机的日子算', () => {
  // 上海的 2026-10-07：00:00 = UTC 前一天 16:00
  const TZ = 'Asia/Shanghai'
  const utc = (day: number, hour: number) => Date.UTC(2026, 9, day, hour, 0)
  const items = [
    { key: 1, at: utc(6, 15) }, // 上海 10-06 23:00
    { key: 2, at: utc(7, 1) }, // 上海 10-07 09:00
    { key: 3, at: utc(6, 16) }, // 上海 10-07 00:00
    { key: 4, at: utc(7, 17) }, // 上海 10-08 01:00
    { key: 5, at: utc(8, 2) }, // 上海 10-08 10:00
  ]

  test('只取这一天的，按时间先后', () => {
    expect(airingsOnDay(items, '2026-10-07', TZ).map((i) => i.key)).toEqual([3, 2])
  })

  test('下一天与那天有几集', () => {
    expect(dayAfter(items, '2026-10-07', TZ)).toEqual({ dayKey: '2026-10-08', count: 2 })
    expect(dayAfter(items, '2026-10-09', TZ)).toEqual({ dayKey: '2026-10-10', count: 0 })
  })

  test('日子不是真实日期时没有下一天', () => {
    expect(dayAfter(items, '今天', TZ)).toBeNull()
  })
})
