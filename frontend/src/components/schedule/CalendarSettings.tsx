import { useEffect, useId, useRef, useState } from 'react'
import { Icon } from '../ui/Icon'
import { Switch } from '../ui'
import type { CalendarPreferences, ScheduleStatus } from './types'

const STORAGE_KEY = 'nagare-calendar-preferences'
export const STATUS_LABELS: Record<ScheduleStatus, string> = { watching: '在看', planning: '想看', completed: '看完', dropped: '弃番', untracked: '未收藏' }
const DEFAULTS: CalendarPreferences = { weekStartsOn: 1, statuses: ['watching', 'planning', 'completed', 'dropped', 'untracked'], indicateWatched: true, disableTransitions: false }

export function useCalendarPreferences() {
  const [preferences, setPreferences] = useState<CalendarPreferences>(() => {
    try {
      const saved = JSON.parse(window.localStorage.getItem(STORAGE_KEY) ?? 'null')
      if (!saved || typeof saved !== 'object') return DEFAULTS
      const statuses: ScheduleStatus[] = Array.isArray(saved.statuses)
        ? [...new Set<ScheduleStatus>(saved.statuses.map((s: string) => s === 'paused' ? 'dropped' : s).filter((s: string) => Object.hasOwn(STATUS_LABELS, s)))]
        : DEFAULTS.statuses
      // 旧演示版没有未收藏状态；迁移后不能把匿名用户的真实日程全部滤掉。
      if (saved.version !== 2 && statuses.length > 0 && !statuses.includes('untracked')) statuses.push('untracked')
      return { weekStartsOn: saved.weekStartsOn === 0 ? 0 : 1, statuses,
        indicateWatched: typeof saved.indicateWatched === 'boolean' ? saved.indicateWatched : true,
        disableTransitions: saved.disableTransitions === true }
    } catch { return DEFAULTS }
  })
  useEffect(() => { try { window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ ...preferences, version: 2 })) } catch { /* 禁用存储时仍可在本页使用 */ } }, [preferences])
  return [preferences, setPreferences] as const
}

export function CalendarSettings({ value, onChange }: { value: CalendarPreferences; onChange: (value: CalendarPreferences) => void }) {
  const [open, setOpen] = useState(false)
  const container = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const id = useId()
  useEffect(() => {
    if (!open) return
    const outside = (e: PointerEvent) => { if (!container.current?.contains(e.target as Node)) setOpen(false) }
    const escape = (e: KeyboardEvent) => { if (e.key === 'Escape') { setOpen(false); trigger.current?.focus() } }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', escape)
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape) }
  }, [open])
  return <div className="calendar-settings" ref={container} onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false) }}>
    <button type="button" className="icon-button" aria-label="日历设置" aria-expanded={open} aria-controls={id} ref={trigger} onClick={() => setOpen(!open)}><Icon name="settings" size={19} /></button>
    {open && <div className="calendar-settings-panel" id={id} role="region" aria-label="日历设置">
      <fieldset><legend>每周开始于</legend>{([1, 0] as const).map(day => <label key={day}>
        <input type="radio" name={id} checked={value.weekStartsOn === day} onChange={() => onChange({ ...value, weekStartsOn: day })} />{day === 1 ? '周一' : '周日'}
      </label>)}</fieldset>
      <fieldset><legend>观看状态</legend><div className="calendar-status-options">{Object.entries(STATUS_LABELS).map(([status, label]) => <label key={status}>
        <input type="checkbox" checked={value.statuses.includes(status as ScheduleStatus)} onChange={(e) => onChange({ ...value,
          statuses: e.target.checked ? [...value.statuses, status as ScheduleStatus] : value.statuses.filter(s => s !== status) })} />{label}
      </label>)}</div></fieldset>
      <div className="calendar-setting-switch"><span>标记已看剧集</span><Switch checked={value.indicateWatched} onChange={checked => onChange({ ...value, indicateWatched: checked })} label="标记已看剧集" /></div>
      <div className="calendar-setting-switch"><span>关闭图片切换</span><Switch checked={value.disableTransitions} onChange={checked => onChange({ ...value, disableTransitions: checked })} label="关闭图片切换" /></div>
    </div>}
  </div>
}
