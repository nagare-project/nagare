// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { ScheduleCalendar } from './ScheduleCalendar'
import type { ScheduleEvent, ScheduleStatus } from './types'

const now = new Date(2026, 8, 4, 12)
const events: ScheduleEvent[] = Array.from({ length: 5 }, (_, index) => ({
  id: String(index), media: { id: index, title: `番剧 ${index}`, episodes: 12, watched: 1, genres: [] },
  episode: index + 1, airingAt: new Date(2026, 8, 4, 19, index).toISOString(),
  status: (index ? 'planning' : 'watching') as ScheduleStatus, watched: index === 0, finale: false,
}))
beforeEach(() => {
  // Node 26 的 Web Storage 会遮住 jsdom 的同名属性，提供隔离的浏览器存储。
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value), clear: () => storage.clear() })
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })
const click = async (container: HTMLElement, selector: string) => act(async () => container.querySelector<HTMLButtonElement>(selector)!.click())

describe('放送日历交互', () => {
  it('翻月、回本月、状态筛选及周首偏好在重新挂载后仍生效', async () => {
    let view = await mount(<ScheduleCalendar events={events} now={now} />)
    await click(view.container, '[aria-label="上个月"]')
    expect(view.container.querySelector('.calendar-month time')?.getAttribute('datetime')).toBe('2026-08')
    expect(view.container.querySelector('.calendar-empty')?.textContent).toContain('没有符合')
    await click(view.container, '.calendar-return')
    await click(view.container, '[aria-label="日历设置"]')
    await click(view.container, 'input[type="radio"]:not(:checked)')
    expect(view.container.querySelector('.calendar-weekdays')?.firstChild?.textContent).toBe('周日')
    await click(view.container, 'input[type="checkbox"]')
    expect(view.container.querySelector('.calendar-grid')?.textContent).not.toContain('番剧 0')
    await view.unmount()
    view = await mount(<ScheduleCalendar events={events} now={now} />)
    expect(view.container.querySelector('.calendar-weekdays')?.firstChild?.textContent).toBe('周日')
    expect(view.container.querySelector('.calendar-grid')?.textContent).not.toContain('番剧 0')
    await view.unmount()
  })
  it('更多剧集打开完整当天列表，关闭详情恢复触发按钮焦点', async () => {
    // jsdom 尚未实现原生 dialog；只补浏览器开关/close 事件，不替代业务逻辑。
    const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
    const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function () { this.setAttribute('open', '') } })
    Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: function () {
      if (!this.open) return
      this.removeAttribute('open'); this.dispatchEvent(new Event('close'))
    } })
    const view = await mount(<ScheduleCalendar events={events} now={now} />)
    const more = view.container.querySelector<HTMLButtonElement>('.calendar-more')!
    await act(async () => { more.focus(); more.click() })
    expect(view.container.querySelector('.calendar-dialog[open]')).not.toBeNull()
    expect(view.container.querySelectorAll('.calendar-dialog .schedule-event')).toHaveLength(5)
    expect(view.container.querySelector('.calendar-dialog')?.textContent).toContain('19:04')
    const preview = view.container.querySelector<HTMLDialogElement>('.calendar-dialog .media-preview')!
    await act(async () => { preview.showModal(); preview.close() })
    // React 的 close 事件会沿组件树传播，关作品预览不能顺带关闭当天详情。
    expect(view.container.querySelector('.calendar-dialog[open]')).not.toBeNull()
    await click(view.container, '[aria-label="关闭当天详情"]')
    expect(view.container.querySelector('.calendar-dialog[open]')).toBeNull()
    expect(document.activeElement).toBe(more)
    await view.unmount()
    if (show) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', show)
    else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal')
    if (close) Object.defineProperty(HTMLDialogElement.prototype, 'close', close)
    else Reflect.deleteProperty(HTMLDialogElement.prototype, 'close')
  })
})

it('旧日历偏好迁移后仍展示未收藏的真实放送', async () => {
 localStorage.setItem('nagare-calendar-preferences', JSON.stringify({ statuses: ['watching', 'planning', 'paused'], weekStartsOn: 0 }))
 const view = await mount(<ScheduleCalendar now={now} events={[{ ...events[0]!, status: 'untracked' }]} />)
 expect(view.container.querySelectorAll('.calendar-event')).toHaveLength(1)
 const saved = JSON.parse(localStorage.getItem('nagare-calendar-preferences')!)
 expect(saved.statuses).toEqual(['watching', 'planning', 'dropped', 'untracked'])
 expect(saved.version).toBe(2)
 await view.unmount()
})
