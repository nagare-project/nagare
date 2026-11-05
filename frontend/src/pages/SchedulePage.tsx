import { useEffect, useMemo, useState } from 'react'
import { CatalogNotice } from '../components/catalog/CatalogStates'
import { FreshnessNote } from '../components/catalog/FreshnessNote'
import { useCollection } from '../components/media/CollectionContext'
import { ScheduleCalendar } from '../components/schedule/ScheduleCalendar'
import { ScheduleEpisodeStrip } from '../components/schedule/ScheduleEpisodeStrip'
import type { ScheduleEvent } from '../components/schedule/types'
import { useCatalogResource } from '../hooks/useCatalogResource'
import { fetchSchedule } from '../lib/endpoints'
import '../components/catalog/catalog.css'
import '../components/media/media.css'
import '../components/schedule/schedule.css'

/** 放送安排会变：开着的页面每半小时、回到窗口时在后台重取一次 */
const SCHEDULE_REFRESH_MS = 30 * 60_000
/** 「几分钟后」每分钟走一格 */
const CLOCK_TICK_MS = 60_000
/** 「即将播出」最多列几集 */
const UPCOMING_LIMIT = 20

/** 放送表（`/schedule`，DESIGN.md「放送表」）：近七日的上游安排，按本地年月日分桶；只表示安排，不推断缺集或已看 */
export function SchedulePage() {
  const [now, setNow] = useState(() => new Date())
  const collection = useCollection()
  const schedule = useCatalogResource('schedule', fetchSchedule, { fallbackError: '放送表暂时无法读取', refreshMs: SCHEDULE_REFRESH_MS, refreshOnFocus: true })
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), CLOCK_TICK_MS)
    return () => window.clearInterval(timer)
  }, [])
  const events = useMemo<ScheduleEvent[]>(() => (schedule.data?.airings ?? []).map(airing => {
    const entry = collection.entries.find(entry => entry.anilistId === airing.anilistId)
    return { id: `${airing.anilistId}:${airing.episode}:${airing.airingAt}`, episode: airing.episode,
      airingAt: new Date(airing.airingAt * 1000).toISOString(),
      status: entry ? (entry.status === 'plan_to_watch' ? 'planning' : entry.status) : 'untracked',
      // 最大观看集号不证明这一集已看；这里不推断逐集标记。
      watched: false, finale: false,
      media: { id: airing.anilistId, title: airing.title, cover: airing.cover, posterTones: airing.posterTones, format: airing.format, episodes: entry?.media.episodes ?? null, watched: 0, genres: [] } }
  }), [schedule.data, collection.entries])
  const upcoming = events.filter(event => new Date(event.airingAt) > now).sort((a, b) => a.airingAt.localeCompare(b.airingAt)).slice(0, UPCOMING_LIMIT).map(event => ({ event }))

  return <main className="catalog-page">
    <header className="catalog-head">
      <div>
        <h1 className="catalog-title">放送表</h1>
        <p className="catalog-subline">
          <span>近七日的上游安排 · 时间按本机时区显示 · 没列出的日子不代表停播</span>
          <FreshnessNote freshness={schedule.data?.freshness} refreshing={schedule.refreshing} />
        </p>
      </div>
    </header>
    {schedule.loading && <div className="schedule-skeleton" role="status" aria-busy="true"><span className="visually-hidden">正在读取放送表</span></div>}
    {schedule.error && <CatalogNotice tone="error" title="放送表暂时无法读取" actions={<button type="button" className="btn" onClick={schedule.retry}>重试</button>}>{schedule.error}</CatalogNotice>}
    {schedule.data && events.length === 0 && <CatalogNotice title="上游暂未返回放送安排">近七日没有拿到任何一集的安排，稍后再来看看。</CatalogNotice>}
    {schedule.data && events.length > 0 && <>
      <section aria-label="放送日历">
        <ScheduleCalendar events={events} now={now} />
      </section>
      <ScheduleEpisodeStrip title="即将播出" items={upcoming} now={now} />
    </>}
  </main>
}
