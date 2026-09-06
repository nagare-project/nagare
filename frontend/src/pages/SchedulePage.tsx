import { useEffect, useMemo, useState } from 'react'
import { fetchSchedule } from '../lib/endpoints'
import type { ScheduleData } from '../lib/endpoints'
import { errorText } from '../lib/format'
import { useCollection } from '../components/media/CollectionContext'
import { ScheduleCalendar } from '../components/schedule/ScheduleCalendar'
import { ScheduleEpisodeStrip } from '../components/schedule/ScheduleEpisodeStrip'
import { ScheduleAgenda } from '../components/schedule/ScheduleEventList'
import type { ScheduleEvent } from '../components/schedule/types'
import { calendarDays, shiftMonth } from '../components/schedule/calendar'
import '../components/library/library.css'
import '../components/media/media.css'
import '../components/schedule/schedule.css'

export function SchedulePage({ embedded = false }: { embedded?: boolean } = {}) {
  const [now, setNow] = useState(() => new Date())
  const [data, setData] = useState<ScheduleData>()
  const [error, setError] = useState<string>()
  const [attempt, setAttempt] = useState(0)
  const collection = useCollection()
  useEffect(() => { const timer = window.setInterval(() => setNow(new Date()), 60_000); return () => window.clearInterval(timer) }, [])
  useEffect(() => {
    const abort = new AbortController()
    let version = 0
    const load = () => { const current = ++version; void fetchSchedule(abort.signal).then(next => { if (!abort.signal.aborted && current === version) { setData(next); setError(undefined) } })
      .catch(err => { if (!abort.signal.aborted && current === version) setError(errorText(err, '放送表暂时无法读取')) }) }
    load(); const timer = window.setInterval(load, 30 * 60_000)
    window.addEventListener('focus', load)
    return () => { abort.abort(); window.clearInterval(timer); window.removeEventListener('focus', load) }
  }, [attempt])
  const events = useMemo<ScheduleEvent[]>(() => (data?.airings ?? []).map(airing => {
    const entry = collection.entries.find(entry => entry.anilistId === airing.anilistId)
    return { id: `${airing.anilistId}:${airing.episode}:${airing.airingAt}`, episode: airing.episode,
      airingAt: new Date(airing.airingAt * 1000).toISOString(),
      status: entry ? (entry.status === 'plan_to_watch' ? 'planning' : entry.status) : 'untracked',
      // 最大观看集号不证明这一集已看；这里不推断逐集标记。
      watched: false, finale: false,
      media: { id: airing.anilistId, title: airing.title, cover: airing.cover, format: airing.format, episodes: entry?.media.episodes ?? null, watched: 0, genres: [] } }
  }), [data, collection.entries])
  const notice = <>
    {error && <p className="result result--err" role="alert">{error} <button className="btn" onClick={() => setAttempt(n => n + 1)}>重试</button></p>}
    {!data && !error && <p role="status">正在读取放送表…</p>}
    {data && events.length === 0 && <p role="status">上游暂未返回放送安排。</p>}
  </>
  if (embedded) {
    const start = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    const end = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 14)
    const uniqueDays = new Map(calendarDays(now, 1, events, now).concat(calendarDays(shiftMonth(now, 1), 1, events, now)).map(day => [day.key, day]))
    const days = [...uniqueDays.values()].filter(day => day.date >= start && day.date < end)
    return <section className="discover-schedule"><h2>放送时间表</h2>{notice}<ScheduleAgenda days={days} indicateWatched={false} discover /></section>
  }
  const upcoming = events.filter(event => new Date(event.airingAt) > now).sort((a,b) => a.airingAt.localeCompare(b.airingAt)).slice(0, 20).map(event => ({ event }))
  return <main className="lib-shell schedule-shell">
    <h1 className="visually-hidden">放送表</h1>{notice}
    <section className="schedule-release" aria-label="放送日历">
      <header className="schedule-section-heading"><div><h2>放送日程</h2><p>上游提供的近七日日程 · 时间以本机时区显示 · 未返回的日期不代表停播</p></div></header>
      <ScheduleCalendar events={events} now={now} />
    </section>
    <ScheduleEpisodeStrip title="即将播出的剧集" items={upcoming} now={now} />
  </main>
}
