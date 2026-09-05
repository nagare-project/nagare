import { FAKE_LISTS, FAKE_SHOWS } from './library'
import type { ScheduleEpisode, ScheduleEvent, ScheduleStatus } from '../../components/schedule/types'

/** FIXME(G3)：放送日期、缺集和即将播出均为演示，不读取或修改用户媒体库。 */
export function fakeSchedule(now = new Date()) {
  const start = new Date(now.getFullYear(), now.getMonth(), 1, 12)
  start.setDate(start.getDate() - (start.getDay() + 6) % 7 - 14)
  // 同一天安排多部作品，覆盖背景轮换和「更多」交互；日期始终固定在本月附近。
  const plan = [[0, 2, 22], [4, 3, 22], [6, 4, 0], [10, 4, 23], [1, 4, 21],
    [2, 4, 22], [5, 4, 23], [7, 5, 21], [9, 6, 22]] as const
  const events: ScheduleEvent[] = []
  for (const [index, day, hour] of plan) {
    const media = FAKE_SHOWS[index]!
    const status = (Object.keys(FAKE_LISTS) as ScheduleStatus[]).find(s => FAKE_LISTS[s]?.some(m => m.id === media.id)) ?? 'planning'
    for (let week = 0; week < 11; week++) {
      const date = new Date(start)
      date.setDate(start.getDate() + week * 7 + day)
      date.setHours(hour, index % 2 ? 30 : 0, 0, 0)
      const episode = Math.max(1, Math.min(media.watched - 1, (media.episodes ?? 24) - 10)) + week
      if (media.episodes !== null && episode > media.episodes) continue
      events.push({ id: `${media.id}-${episode}`, media, episode, airingAt: date.toISOString(), status,
        watched: episode <= media.watched, finale: episode === media.episodes })
    }
  }
  events.sort((a, b) => Date.parse(a.airingAt) - Date.parse(b.airingAt))
  const missing: ScheduleEpisode[] = []
  const upcoming: ScheduleEpisode[] = []
  for (const media of FAKE_SHOWS) {
    const past = events.filter(e => e.media.id === media.id && !e.watched && Date.parse(e.airingAt) < now.getTime())
    if (past[0]) missing.push({ event: past[0], extraCount: past.length - 1 })
    const next = events.find(e => e.media.id === media.id && Date.parse(e.airingAt) >= now.getTime())
    if (next) upcoming.push({ event: next })
  }
  upcoming.sort((a, b) => Date.parse(a.event.airingAt) - Date.parse(b.event.airingAt))
  return { events, missing, upcoming }
}
