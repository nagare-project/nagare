import { MediaArtwork } from '../media/MediaArtwork'
import { MediaPreview } from '../media/MediaPreview'
import { Icon } from '../ui/Icon'
import { airingTime, dayLabel } from './calendar'
import type { CalendarDay, ScheduleEvent } from './types'

export function ScheduleEventList({ events, indicateWatched }: { events: ScheduleEvent[]; indicateWatched: boolean }) {
  return <ul className="schedule-event-list">{events.map(event => <li key={event.id}>
    <MediaPreview media={event.media} className={event.watched && indicateWatched ? 'schedule-event schedule-event--watched' : 'schedule-event'}>
      <span className="schedule-event-art"><MediaArtwork src={event.media.cover} title={event.media.title} /></span>
      <span className="schedule-event-info"><span className="schedule-event-title">{event.media.title}
        {event.watched && indicateWatched && <span className="schedule-watched" aria-label="已看"><Icon name="check" size={14} /></span>}
      </span><span className="schedule-event-meta">第 {event.episode} 集 <span>·</span> <time dateTime={event.airingAt}>{airingTime(event)}</time>
        {event.finale && <span className="schedule-finale">· 完结</span>}
      </span></span>
    </MediaPreview>
  </li>)}</ul>
}

export function ScheduleAgenda({ days, indicateWatched, discover = false }: { days: CalendarDay[]; indicateWatched: boolean; discover?: boolean }) {
  const relevant = days.filter(day => day.events.length || day.today)
  if (!relevant.some(day => day.events.length)) return <p className="schedule-empty">这段时间没有安排播出的剧集。</p>
  return <div className={discover ? 'schedule-agenda schedule-agenda--discover' : 'schedule-agenda'}>
    {relevant.map(day => <section className="schedule-agenda-day" key={day.key} aria-label={dayLabel(day.date)}>
      <header className="schedule-agenda-heading">
        <time dateTime={day.key} className={day.today ? 'schedule-date schedule-date--today' : 'schedule-date'}>{day.date.getDate()}</time>
        <div><h3>{day.date.toLocaleDateString('zh-CN', { weekday: 'long' })}{day.today && <span className="schedule-today-label">今天</span>}</h3>
          <p>{day.date.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' })}</p></div>
        {!!day.events.length && <span className="schedule-count">{day.events.length} 集</span>}
      </header>
      {day.events.length ? <ScheduleEventList events={day.events} indicateWatched={indicateWatched} /> : <p className="schedule-empty">今天没有剧集播出。</p>}
    </section>)}
  </div>
}
