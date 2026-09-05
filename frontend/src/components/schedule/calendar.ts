import type { CalendarDay, ScheduleEvent } from './types'

/** 日期分桶始终使用本地年月日，不能切 UTC ISO 字符串。 */
export function localDateKey(date: Date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
export function shiftMonth(date: Date, offset: number) {
  // 先钉住 1 日，避免 1 月 31 日加一个月溢出到 3 月。
  return new Date(date.getFullYear(), date.getMonth() + offset, 1, 12)
}
export function calendarDays(month: Date, weekStartsOn: 0 | 1, events: ScheduleEvent[], now: Date): CalendarDay[] {
  const start = shiftMonth(month, 0)
  start.setDate(1 - (start.getDay() - weekStartsOn + 7) % 7)
  const end = new Date(month.getFullYear(), month.getMonth() + 1, 0, 12)
  end.setDate(end.getDate() + (weekStartsOn + 6 - end.getDay() + 7) % 7)
  const grouped = new Map<string, ScheduleEvent[]>()
  for (const event of events) {
    const key = localDateKey(new Date(event.airingAt))
    grouped.set(key, [...(grouped.get(key) ?? []), event])
  }
  const days: CalendarDay[] = []
  // 按日历天推进，夏令时切换日不一定有 24 小时。
  for (let date = new Date(start); date <= end; date.setDate(date.getDate() + 1)) {
    const key = localDateKey(date)
    days.push({ date: new Date(date), key, currentMonth: date.getMonth() === month.getMonth(),
      today: key === localDateKey(now), events: (grouped.get(key) ?? []).sort((a, b) =>
        Date.parse(a.airingAt) - Date.parse(b.airingAt) || a.episode - b.episode) })
  }
  return days
}
export const dayLabel = (date: Date) => date.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' })
export const airingTime = (event: ScheduleEvent) => new Date(event.airingAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
