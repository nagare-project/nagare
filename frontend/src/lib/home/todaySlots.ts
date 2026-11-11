// 「今日更新」：今天的哪几集已经播了、哪几集快播了，「现在」标记画在它们中间哪里。
//
// 一切与时间有关的都是 nowMs 的函数（由每分钟走一格的时钟给），同样的输入永远得到同样的结果。
// 「今天」按本机时区的日历日算（上游的放送表按它自己服务器的日子分组，nagare 不用那个分组）。
//
// 纯函数：不碰 DOM、不碰 React。

import { clockParts, nextDayKey } from './time'

/** 这个范围内才显示「N 分钟后」和闪动的点 */
export const SOON_WINDOW_MS = 60 * 60_000

export type SlotState = 'aired' | 'soon' | 'later'

export interface Slot<T> {
  item: T
  state: SlotState
  /** 离播出还有几分钟，向上取整；只有 soon 才有 */
  minutesUntil: number | null
}

export interface TodaySlots<T> {
  slots: Slot<T>[]
  /** 「现在」标记画在 slots 里这个下标之前 */
  nowIndex: number
}

export function slotToday<T extends { at: number }>(items: readonly T[], nowMs: number): TodaySlots<T> {
  // 稳定排序的副本：输入不动
  const sorted = [...items].sort((a, b) => a.at - b.at)
  const slots = sorted.map((item): Slot<T> => {
    const delta = item.at - nowMs
    if (delta <= 0) return { item, state: 'aired', minutesUntil: null }
    if (delta <= SOON_WINDOW_MS) return { item, state: 'soon', minutesUntil: Math.ceil(delta / 60_000) }
    return { item, state: 'later', minutesUntil: null }
  })
  const nowIndex = slots.filter((s) => s.state === 'aired').length
  return { slots, nowIndex }
}

/** dayKey 这一天（本机时区）的放送，按时间先后 */
export function airingsOnDay<T extends { at: number }>(items: readonly T[], dayKey: string, timeZone?: string): T[] {
  return items.filter((item) => clockParts(item.at, timeZone).dayKey === dayKey).sort((a, b) => a.at - b.at)
}

/** dayKey 的下一天，以及那天有几集 —— 今日更新那一排的末尾说的「明天」。dayKey 不是真实日期时为 null */
export function dayAfter<T extends { at: number }>(items: readonly T[], dayKey: string, timeZone?: string): { dayKey: string; count: number } | null {
  const next = nextDayKey(dayKey)
  if (!next) return null
  return { dayKey: next, count: airingsOnDay(items, next, timeZone).length }
}
