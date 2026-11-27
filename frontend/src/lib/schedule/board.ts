// 放送表（/schedule）纯数据计算：7 日分桶、每日代表色、按分钟分组的时间线、「我追的 · 本周」状态、周柱状图高度、窄屏滚动偏移。
// 不碰 React，不碰 DOM。

import type { ScheduleAiring } from '../endpoints'
import { clockParts, parseDayKey, weekdayLabel } from '../home/time'
import { slotToday, type Slot } from '../home/todaySlots'
import { FORMAT_LABELS } from '../../components/media/media-format'
import { genreFilter } from '../../components/media/genres'

export const WEEK_DAYS = 7
export const BAR_MAX_PX = 96
export const BAR_MIN_PX = 8

const MINUTE_MS = 60_000
const WEEKDAY_SHORT = ['日', '一', '二', '三', '四', '五', '六'] as const
const pad2 = (n: number) => String(n).padStart(2, '0')

export interface ScheduleItemView {
  key: string
  id: number
  title: string
  ep: number
  /** 播出时刻（epoch ms） */
  at: number
  cover: string | null
  hue: number | null
  score: number | null
  meta: string
  inLibrary: boolean
}

export interface ScheduleDayView {
  key: string
  label: string
  short: string
  md: string
  isToday: boolean
  hue: number | null
  items: ScheduleItemView[]
}

export interface TimeGroup<T> {
  at: number
  slots: Slot<T>[]
  aired: boolean
}

export interface DayTimeline<T> {
  groups: TimeGroup<T>[]
  nowIndex: number
}

export type FollowState = 'unwatched' | 'watched' | 'behind' | 'upcoming'

export interface FollowEntry<T> {
  item: T
  state: FollowState
  behind: number
}

/** YYYY-MM-DD 偏移 n 天 */
export function addDays(dayKey: string, n: number): string {
  const parts = parseDayKey(dayKey)
  if (!parts) return ''
  const d = new Date(Date.UTC(parts.year, parts.month - 1, parts.day + n))
  return `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())}`
}

/** 连续 7 个日历日（今天起） */
export function weekKeys(todayKey: string): string[] {
  if (!parseDayKey(todayKey)) return []
  return Array.from({ length: WEEK_DAYS }, (_, i) => addDays(todayKey, i))
}

export function monthDay(dayKey: string): string {
  const parts = parseDayKey(dayKey)
  return parts ? `${parts.month}/${parts.day}` : ''
}

export function weekdayShort(weekday: number): string {
  return WEEKDAY_SHORT[weekday] ?? ''
}

/** 挑一天里评分最高的有色作品作为该日页底主色；平分取更早播出的 */
export function leadOf<T extends { score?: number | null }>(rows: readonly T[]): T | null {
  let lead: T | null = null
  for (const row of rows) {
    if (lead === null) {
      lead = row
      continue
    }
    if ((row.score ?? 0) > (lead.score ?? 0)) lead = row
  }
  return lead
}

export function barHeight(count: number, max: number): number {
  if (max <= 0 || count <= 0) return BAR_MIN_PX
  return Math.max(BAR_MIN_PX, Math.round((BAR_MAX_PX * count) / max))
}

function buildMeta(airing: ScheduleAiring): string {
  const parts: string[] = [airing.episodes ? `第 ${airing.episode} / ${airing.episodes} 集` : `第 ${airing.episode} 集`]
  if (airing.format) parts.push(FORMAT_LABELS[airing.format] ?? airing.format)
  if (airing.genres && airing.genres.length > 0) {
    const labels = airing.genres
      .slice(0, 2)
      .map((g) => genreFilter(g) ?? g)
      .filter(Boolean)
    if (labels.length > 0) parts.push(labels.join(' · '))
  }
  return parts.join(' · ')
}

/** 将 /api/schedule 的扁平放送列表按本机时区分桶成连续 7 天 */
export function buildWeek(airings: readonly ScheduleAiring[], nowMs: number, timeZone?: string): ScheduleDayView[] {
  const todayKey = clockParts(nowMs, timeZone).dayKey
  const keys = weekKeys(todayKey)
  const buckets = new Map<string, ScheduleItemView[]>(keys.map((k) => [k, []]))

  for (const a of airings) {
    const at = a.airingAt * 1000
    const dayKey = clockParts(at, timeZone).dayKey
    const list = buckets.get(dayKey)
    if (!list) continue
    const hue = a.hue ?? a.posterTones?.hue ?? null
    list.push({
      key: `${a.scheduleId ?? a.anilistId}:${a.episode}:${a.airingAt}`,
      id: a.anilistId,
      title: a.title,
      ep: a.episode,
      at,
      cover: a.cover ?? null,
      hue,
      score: a.score ?? null,
      meta: buildMeta(a),
      inLibrary: a.inLibrary,
    })
  }

  return keys.map((key, idx) => {
    const parts = parseDayKey(key)!
    const items = (buckets.get(key) ?? []).sort((a, b) => a.at - b.at)
    const colored = items.filter((it) => it.hue !== null)
    const lead = leadOf(colored)
    const label = idx === 0 ? '今天' : idx === 1 ? '明天' : weekdayLabel(parts.weekday)
    return {
      key,
      label,
      short: idx === 0 ? '今' : weekdayShort(parts.weekday),
      md: monthDay(key),
      isToday: idx === 0,
      hue: lead?.hue ?? null,
      items,
    }
  })
}

/** 按播出分钟分组，并计算「现在」标尺的位置 */
export function dayTimeline<T extends { at: number }>(items: readonly T[], nowMs: number): DayTimeline<T> {
  const groups: TimeGroup<T>[] = []
  for (const slot of slotToday(items, nowMs).slots) {
    const minute = Math.floor(slot.item.at / MINUTE_MS) * MINUTE_MS
    const last = groups[groups.length - 1]
    if (last && last.at === minute) {
      groups[groups.length - 1] = {
        ...last,
        slots: [...last.slots, slot],
        aired: last.aired && slot.state === 'aired',
      }
    } else {
      groups.push({ at: minute, slots: [slot], aired: slot.state === 'aired' })
    }
  }
  const firstOpen = groups.findIndex((g) => !g.aired)
  return { groups, nowIndex: firstOpen === -1 ? groups.length : firstOpen }
}

function stateOf(ep: number, watched: number, aired: boolean): { state: FollowState; behind: number } {
  if (aired) return { state: ep > watched ? 'unwatched' : 'watched', behind: 0 }
  const behind = ep - 1 - watched
  return behind > 0 ? { state: 'behind', behind } : { state: 'upcoming', behind: 0 }
}

/** 找出本周有更新的在追作品（每部只列最早的一集），按播出时间排序 */
export function followingThisWeek<T extends { id: number; at: number; ep: number }>(
  days: ReadonlyArray<{ items: readonly T[] }>,
  progress: Readonly<Record<number, number>>,
  nowMs: number,
): FollowEntry<T>[] {
  const first = new Map<number, T>()
  for (const day of days) {
    for (const it of day.items) {
      if (progress[it.id] === undefined) continue
      const seen = first.get(it.id)
      if (!seen || it.at < seen.at) first.set(it.id, it)
    }
  }
  return [...first.values()]
    .sort((a, b) => a.at - b.at)
    .map((item) => ({ item, ...stateOf(item.ep, progress[item.id], item.at <= nowMs) }))
}

export function rowFollowState(
  ep: number,
  watched: number | undefined,
  aired: boolean,
): 'unwatched' | 'following' | null {
  if (watched === undefined) return null
  return aired && ep > watched ? 'unwatched' : 'following'
}

export function whenLabel(atMs: number, nowMs: number, timeZone?: string): string {
  const at = clockParts(atMs, timeZone)
  const day = at.dayKey === clockParts(nowMs, timeZone).dayKey ? '今天' : weekdayLabel(at.weekday)
  return `${day} ${at.hh}:${at.mm}`
}

/** 横向标签栏滚到露出选中标签的最小 scrollLeft；已在视口内时返回 null */
export function revealScrollLeft(
  m: { viewLeft: number; viewWidth: number; contentWidth: number; itemStart: number; itemWidth: number },
  padPx: number,
): number | null {
  if (m.contentWidth <= m.viewWidth) return null
  const itemEnd = m.itemStart + m.itemWidth
  if (m.itemStart - padPx < m.viewLeft) return Math.max(0, m.itemStart - padPx)
  if (itemEnd + padPx > m.viewLeft + m.viewWidth) {
    return Math.min(m.contentWidth - m.viewWidth, itemEnd + padPx - m.viewWidth)
  }
  return null
}

/** 键盘方向键在 tablist 里的下一个下标；非导航键返回 null */
export function nextTabIndex(key: string, current: number, length: number): number | null {
  if (length <= 0) return null
  switch (key) {
    case 'ArrowRight':
      return (current + 1) % length
    case 'ArrowLeft':
      return (current - 1 + length) % length
    case 'Home':
      return 0
    case 'End':
      return length - 1
    default:
      return null
  }
}
