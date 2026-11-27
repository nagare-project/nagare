import { useEffect, useMemo, useState, type CSSProperties } from 'react'
import { CatalogNotice } from '../components/catalog/CatalogStates'
import { FreshnessNote } from '../components/catalog/FreshnessNote'
import hueScope from '../components/home/HomeHueScope.module.css'
import { useCollection } from '../components/media/CollectionContext'
import { ScheduleBoard } from '../components/schedule/ScheduleBoard'
import board from '../components/schedule/ScheduleBoard.module.css'
import { useCatalogResource } from '../hooks/useCatalogResource'
import { fetchSchedule } from '../lib/endpoints'
import { pageToneVars } from '../lib/home/tone'
import { buildWeek } from '../lib/schedule/board'

/** 放送安排会变：开着的页面每半小时、回到窗口时在后台重取一次 */
const SCHEDULE_REFRESH_MS = 30 * 60_000
/** 「几分钟后」和「现在」标尺每分钟走一格 */
const CLOCK_TICK_MS = 60_000

/** 放送表（`/schedule`）：近七日上游安排，按本机时区分桶，对齐网站 ScheduleBoard 版式 */
export function SchedulePage() {
  const [nowMs, setNowMs] = useState(() => Date.now())
  const collection = useCollection()
  const schedule = useCatalogResource('schedule', fetchSchedule, {
    fallbackError: '放送表暂时无法读取',
    refreshMs: SCHEDULE_REFRESH_MS,
    refreshOnFocus: true,
  })

  useEffect(() => {
    const timer = window.setInterval(() => setNowMs(Date.now()), CLOCK_TICK_MS)
    return () => window.clearInterval(timer)
  }, [])

  const progress = useMemo<Readonly<Record<number, number>> | null>(() => {
    if (!collection.loggedIn) return null
    const map: Record<number, number> = {}
    for (const entry of collection.entries) {
      if (entry.status === 'watching' || entry.status === 'plan_to_watch') {
        map[entry.anilistId] = entry.currentEpisode
      }
    }
    return map
  }, [collection.loggedIn, collection.entries])

  const days = useMemo(
    () => (schedule.data ? buildWeek(schedule.data.airings, nowMs) : []),
    [schedule.data, nowMs],
  )

  const freshnessNode = <FreshnessNote freshness={schedule.data?.freshness} refreshing={schedule.refreshing} />

  if (schedule.data && schedule.data.airings.length > 0) {
    return <ScheduleBoard days={days} nowMs={nowMs} progress={progress} freshnessNode={freshnessNode} />
  }

  return (
    <main className={`${hueScope.scope} ${board.page}`} style={pageToneVars(null) as unknown as CSSProperties}>
      <header className={board.header}>
        <span className={board.eyebrow}>本周排期</span>
        <h1 className={board.title}>放送表</h1>
        <p className={board.sub}>
          <span>近 7 日的上游安排 · 时间按本机时区显示</span>
          {freshnessNode}
        </p>
      </header>
      {schedule.loading && (
        <div className={board.skeleton} role="status" aria-busy="true">
          <span className="visually-hidden">正在读取放送表</span>
          <div className={board.skeletonTabs} aria-hidden="true">
            {Array.from({ length: 7 }, (_, i) => <div key={i} className={board.skeletonBox} />)}
          </div>
          {Array.from({ length: 4 }, (_, i) => <div key={i} className={board.skeletonRow} aria-hidden="true" />)}
        </div>
      )}
      {schedule.error && (
        <div className={board.noticeWrap}>
          <CatalogNotice
            tone="error"
            title="放送表暂时无法读取"
            actions={<button type="button" className="btn" onClick={schedule.retry}>重试</button>}
          >
            {schedule.error}
          </CatalogNotice>
        </div>
      )}
      {schedule.data && schedule.data.airings.length === 0 && (
        <div className={board.noticeWrap}>
          <CatalogNotice title="上游暂未返回放送安排">近七日没有拿到任何一集的安排，稍后再来看看。</CatalogNotice>
        </div>
      )}
    </main>
  )
}
