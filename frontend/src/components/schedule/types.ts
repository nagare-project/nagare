import type { MediaSummary } from '../media/types'

/** 日历展示契约；由真实接口或演示数据适配，组件不依赖 fixture。 */
export type ScheduleStatus = 'watching' | 'planning' | 'completed' | 'paused'
export interface ScheduleEvent {
  id: string
  media: MediaSummary
  episode: number
  airingAt: string
  status: ScheduleStatus
  watched: boolean
  finale: boolean
}
export interface CalendarDay {
  date: Date
  key: string
  currentMonth: boolean
  today: boolean
  events: ScheduleEvent[]
}
export interface CalendarPreferences {
  weekStartsOn: 0 | 1
  statuses: ScheduleStatus[]
  indicateWatched: boolean
  disableTransitions: boolean
}
export interface ScheduleEpisode {
  event: ScheduleEvent
  extraCount?: number
}
