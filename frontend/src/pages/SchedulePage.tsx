import { useMemo, useState } from 'react'
import { FixtureNotice } from './ListsPage'
import { fakeSchedule } from '../lib/fixtures/schedule'
import { ScheduleCalendar } from '../components/schedule/ScheduleCalendar'
import { ScheduleEpisodeStrip } from '../components/schedule/ScheduleEpisodeStrip'
import { ScheduleAgenda } from '../components/schedule/ScheduleEventList'
import { calendarDays, shiftMonth } from '../components/schedule/calendar'
import '../components/library/library.css'
import '../components/media/media.css'
import '../components/schedule/schedule.css'

/** FIXME(G3)：复刻展示和交互，日期、缺集仍为演示数据，真实放送接口待接入。 */
export function SchedulePage({ embedded = false }: { embedded?: boolean } = {}) {
  const [now] = useState(() => new Date())
  const { events, missing, upcoming } = useMemo(() => fakeSchedule(now), [now])

  // seanime 的 Discover 标签是近期播出列表；独立 Schedule 才是月历。
  if (embedded) {
    const start = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    const end = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 14)
    const uniqueDays = new Map(calendarDays(now, 1, events, now)
      .concat(calendarDays(shiftMonth(now, 1), 1, events, now)).map(day => [day.key, day]))
    const days = [...uniqueDays.values()].filter(day => day.date >= start && day.date < end)
    return <><FixtureNotice gap="G3" what="播出时间" /><section className="discover-schedule">
      <h2>放送时间表</h2><ScheduleAgenda days={days} indicateWatched={false} discover />
    </section></>
  }

  return <main className="lib-shell schedule-shell">
    <h1 className="visually-hidden">放送表</h1>
    <FixtureNotice gap="G3" what="播出时间和缺集" />
    <ScheduleEpisodeStrip title="媒体库缺集" items={missing} missing now={now} />
    <section className="schedule-release" aria-label="放送日历">
      <header className="schedule-section-heading"><div><h2>放送日程</h2><p>根据演示追番列表 · 时间以本机时区显示</p></div></header>
      <ScheduleCalendar events={events} now={now} />
    </section>
    <ScheduleEpisodeStrip title="即将播出的剧集" items={upcoming} now={now} />
  </main>
}
