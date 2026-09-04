import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { MediaArtwork } from '../media/MediaArtwork'
import { MediaPreview } from '../media/MediaPreview'
import { useReducedMotionPreference } from '../media/useReducedMotionPreference'
import { Icon } from '../ui/Icon'
import { CalendarSettings, useCalendarPreferences } from './CalendarSettings'
import { ScheduleAgenda, ScheduleEventList } from './ScheduleEventList'
import { airingTime, calendarDays, dayLabel, shiftMonth } from './calendar'
import type { CalendarDay, ScheduleEvent } from './types'

export function ScheduleCalendar({ events, now }: { events: ScheduleEvent[]; now: Date }) {
  const [month, setMonth] = useState(() => shiftMonth(now, 0))
  const [preferences, setPreferences] = useCalendarPreferences()
  const reduced = useReducedMotionPreference()
  const [selectedDay, setSelectedDay] = useState<CalendarDay | null>(null)
  const days = useMemo(() => calendarDays(month, preferences.weekStartsOn,
    events.filter(e => preferences.statuses.includes(e.status)), now), [month, preferences.weekStartsOn, preferences.statuses, events, now])
  const weekdays = preferences.weekStartsOn === 1 ? ['周一', '周二', '周三', '周四', '周五', '周六', '周日'] : ['周日', '周一', '周二', '周三', '周四', '周五', '周六']
  const monthLabel = month.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long' })
  const currentMonth = month.getMonth() === now.getMonth() && month.getFullYear() === now.getFullYear()
  return <div className="schedule-calendar">
    <header className="calendar-header">
      <button type="button" className="icon-button calendar-arrow" aria-label="上个月" onClick={() => setMonth(m => shiftMonth(m, -1))}><Icon name="left" size={18} /></button>
      <h2 className={currentMonth ? 'calendar-month calendar-month--current' : 'calendar-month'} aria-live="polite"><time dateTime={`${month.getFullYear()}-${String(month.getMonth() + 1).padStart(2, '0')}`}>{monthLabel}</time></h2>
      <div className="calendar-header-actions">
        {!currentMonth && <button type="button" className="calendar-return" onClick={() => setMonth(shiftMonth(now, 0))}>本月</button>}
        <button type="button" className="icon-button calendar-arrow" aria-label="下个月" onClick={() => setMonth(m => shiftMonth(m, 1))}><Icon name="right" size={18} /></button>
        <CalendarSettings value={preferences} onChange={setPreferences} />
      </div>
    </header>
    <div className="calendar-weekdays" aria-hidden="true">{weekdays.map(day => <span key={day}>{day}</span>)}</div>
    <div className="calendar-grid" aria-label={monthLabel}>
      {days.map(day => <ScheduleDay key={day.key} day={day} indicateWatched={preferences.indicateWatched}
        disableTransitions={preferences.disableTransitions || reduced} onOpen={() => setSelectedDay(day)} />)}
    </div>
    <div className="calendar-mobile"><ScheduleAgenda days={days} indicateWatched={preferences.indicateWatched} /></div>
    {!days.some(day => day.currentMonth && day.events.length > 0) && <p className="calendar-empty" role="status">本月没有符合当前筛选条件的放送安排。</p>}
    <CalendarDayDialog day={selectedDay} indicateWatched={preferences.indicateWatched} onClose={() => setSelectedDay(null)} />
  </div>
}

function ScheduleDay({ day, indicateWatched, disableTransitions, onOpen }: {
  day: CalendarDay; indicateWatched: boolean; disableTransitions: boolean; onOpen: () => void
}) {
  const [index, setIndex] = useState(0)
  const [hovered, setHovered] = useState<string | null>(null)
  useEffect(() => {
    if (disableTransitions || day.events.length < 2) return
    const timer = setInterval(() => { if (!document.hidden) setIndex(i => (i + 1) % day.events.length) }, 5000)
    return () => clearInterval(timer)
  }, [disableTransitions, day.events.length])
  const hoveredEvent = day.events.find(event => event.id === hovered)
  const shown = hoveredEvent ?? day.events[disableTransitions ? 0 : index % day.events.length]
  return <section className={['calendar-day', !day.currentMonth && 'calendar-day--outside', day.today && 'calendar-day--today', disableTransitions && 'calendar-day--still'].filter(Boolean).join(' ')} data-date={day.key} aria-label={dayLabel(day.date)}>
    {!!day.events.length && <div className="calendar-day-art" aria-hidden="true">
      {day.events.map(event => <span key={event.id} className={shown?.id === event.id ? 'calendar-day-image calendar-day-image--on' : 'calendar-day-image'}>
        <MediaArtwork src={event.media.cover} title={event.media.title} />
      </span>)}
    </div>}
    <button type="button" className="calendar-day-open" aria-label={`${dayLabel(day.date)}，${day.events.length} 集放送，查看当天详情`} onClick={onOpen} />
    <time dateTime={day.key} aria-current={day.today ? 'date' : undefined} className={day.today ? 'schedule-date schedule-date--today' : 'schedule-date'}>{day.date.getDate()}</time>
    {hoveredEvent && <div className="calendar-event-tooltip" role="tooltip">{hoveredEvent.media.title} · 第 {hoveredEvent.episode} 集 · {airingTime(hoveredEvent)}{hoveredEvent.finale && ' · 完结'}</div>}
    {!!day.events.length && <ol className="calendar-events">{day.events.slice(0, 4).map(event => <li key={event.id}
      onMouseEnter={() => setHovered(event.id)} onMouseLeave={() => setHovered(null)}
      onFocus={() => setHovered(event.id)} onBlur={() => setHovered(null)}>
      <MediaPreview media={event.media} className={indicateWatched && event.watched ? 'calendar-event calendar-event--watched' : 'calendar-event'}>
        {indicateWatched && event.watched && <span className="schedule-watched" aria-label="已看"><Icon name="check" size={12} /></span>}
        {event.finale && !event.watched && <span className="schedule-finale" aria-label="完结"><Icon name="flag" size={12} /></span>}
        <span className="calendar-event-name">{event.media.title}</span><span className="calendar-event-episode">第 {event.episode} 集</span>
      </MediaPreview>
    </li>)}{day.events.length > 4 && <li><button type="button" className="calendar-more" onClick={onOpen}>+ {day.events.length - 4} 部作品</button></li>}</ol>}
  </section>
}

function CalendarDayDialog({ day, indicateWatched, onClose }: { day: CalendarDay | null; indicateWatched: boolean; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  const isOpen = day !== null
  useEffect(() => {
    if (!isOpen) return
    const trigger = document.activeElement as HTMLElement | null
    dialog.current?.showModal()
    return () => { dialog.current?.close(); trigger?.focus() }
  }, [isOpen])
  return <dialog ref={dialog} className="calendar-dialog" aria-labelledby={titleId} onClose={event => { if (event.target === event.currentTarget) onClose() }}
    onClick={e => { if (e.target === dialog.current) dialog.current.close() }}>
    {day && <><header><h2 id={titleId}>{dayLabel(day.date)}</h2><p>{day.events.length} 集放送</p>
      <button type="button" className="icon-button" aria-label="关闭当天详情" onClick={() => dialog.current?.close()}><Icon name="close" /></button></header>
      {day.events.length ? <ScheduleEventList events={day.events} indicateWatched={indicateWatched} /> : <p className="schedule-empty">这一天没有剧集播出。</p>}
    </>}
  </dialog>
}
