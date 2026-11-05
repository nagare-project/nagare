// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CatalogFreshness, LibraryData } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { getAllByRole, getAllByText, getByRole, queryByRole } from '../../test/queries'
import { installLocalStorage } from '../../test/storage'
import { CatalogNotice } from './CatalogStates'
import { EmptyLibraryStrip } from './EmptyLibraryStrip'
import { ANNOUNCE_CLEAR_MS, BACK_ONLINE_TEXT, FreshnessNote } from './FreshnessNote'

vi.mock('../../lib/endpoints', async (importOriginal) => {
  const original = await importOriginal<typeof import('../../lib/endpoints')>()
  return { ...original, fetchLibrary: vi.fn(async (): Promise<LibraryData> => ({ folders: [], clusters: [], continueWatching: [], scannedAt: null } as unknown as LibraryData)) }
})

// 「离线 · 更新于 …」里的相对时间用的是本机时钟：钉住，结果不随运行时刻变
const NOW = Date.parse('2026-11-06T12:00:00Z')
const minutesAgo = (minutes: number) => new Date(NOW - minutes * 60_000).toISOString()
const offline = (minutes: number): CatalogFreshness => ({ fetchedAt: minutesAgo(minutes), stale: false, offline: true })
const fresh = (): CatalogFreshness => ({ fetchedAt: minutesAgo(0), stale: false, offline: false })
const stale = (minutes: number): CatalogFreshness => ({ fetchedAt: minutesAgo(minutes), stale: true, offline: false })

/** 细条的播报区（读屏会念的那一处）：只有它是 live region */
function announcement(container: HTMLElement): string {
  return getByRole(container, 'status').textContent ?? ''
}

/** 看得见的那句细条（不算旁边 visually-hidden 的播报区） */
function shownNote(container: HTMLElement): string | null {
  const status = getByRole(container, 'status')
  const notes = getAllByText(container, /更新于|离线/).filter((el) => el !== status)
  if (notes.length > 1) throw new Error(`细条找到 ${notes.length} 句`)
  return notes[0]?.textContent ?? null
}

describe('整块的状态说明（CatalogNotice）', () => {
  it('出错时标题仍是标题，播报挂在另一层：读屏既能按标题跳到这里，也会马上念出原因', async () => {
    const { container, unmount } = await mount(
      <CatalogNotice tone="error" title="连不上目录服务，媒体库照常可用" actions={<button type="button">重试</button>}>
        发现页的作品来自在线目录。
      </CatalogNotice>,
    )
    const heading = getByRole(container, 'heading', { level: 2, name: '连不上目录服务，媒体库照常可用' })
    expect(heading.hasAttribute('role')).toBe(false)
    const alert = getByRole(container, 'alert')
    expect(alert.contains(heading)).toBe(true)
    expect(alert.textContent).toBe('连不上目录服务，媒体库照常可用发现页的作品来自在线目录。')
    // 按钮不在播报里：念出来的是发生了什么，不是一串按钮名
    expect(alert.contains(getByRole(container, 'button', { name: '重试' }))).toBe(false)
    await unmount()
  })

  it('不是出错（空状态）时只有标题与说明，不播报', async () => {
    const { container, unmount } = await mount(<CatalogNotice title="这一季暂时没有作品">换一季看看。</CatalogNotice>)
    expect(getByRole(container, 'heading', { level: 2, name: '这一季暂时没有作品' })).toBeDefined()
    expect(queryByRole(container, 'alert')).toBeNull()
    await unmount()
  })
})

describe('「离线 · 更新于 …」细条（FreshnessNote）', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(NOW)
  })
  afterEach(() => vi.useRealTimers())

  it('进入离线时念一次；之后相对时间每分钟走一格，看得见的字在变，读屏不再重念', async () => {
    const { container, unmount } = await mount(<FreshnessNote freshness={offline(5)} />)
    expect(shownNote(container)).toBe('离线 · 更新于 5 分钟前')
    expect(announcement(container)).toBe('离线 · 更新于 5 分钟前')

    // 念完一会儿播报区清空（逐行浏览时不重复念、也不留一句停在「5 分钟前」的旧话）
    await act(async () => { vi.advanceTimersByTime(ANNOUNCE_CLEAR_MS) })
    expect(announcement(container)).toBe('')
    await act(async () => { vi.advanceTimersByTime(60_000) })
    expect(shownNote(container)).toBe('离线 · 更新于 6 分钟前')
    expect(announcement(container)).toBe('')
    // 看得见的那句不是 live region：它每分钟变一次
    expect(getAllByRole(container, 'status')).toHaveLength(1)
    await unmount()
  })

  it('离线 → 重新连上：念一句「已重新连上目录服务」，细条让出的位置留着', async () => {
    const { container, rerender, unmount } = await mount(<FreshnessNote freshness={offline(30)} />)
    await rerender(<FreshnessNote freshness={fresh()} />)
    expect(announcement(container)).toBe(BACK_ONLINE_TEXT)
    expect(shownNote(container)).toBeNull()
    await unmount()
  })

  it('离线时换了一份正在读（季度页换季度时数据先清空）：不算重新连上，不念「已重新连上」', async () => {
    const { container, rerender, unmount } = await mount(<FreshnessNote freshness={offline(30)} />)
    await act(async () => { vi.advanceTimersByTime(ANNOUNCE_CLEAR_MS) })
    await rerender(<FreshnessNote freshness={undefined} />)
    expect(announcement(container)).toBe('')
    // 新的一份还是离线的：已经在离线里了，不再念一遍
    await rerender(<FreshnessNote freshness={offline(40)} />)
    expect(announcement(container)).toBe('')
    expect(shownNote(container)).toBe('离线 · 更新于 40 分钟前')
    await unmount()
  })

  it('在线时不念：过期的一份「正在刷新」来去、数据换新都不出声；后来断网才念', async () => {
    const { container, rerender, unmount } = await mount(<FreshnessNote freshness={stale(60)} refreshing />)
    expect(shownNote(container)).toBe('更新于 1 小时前 · 正在刷新')
    expect(announcement(container)).toBe('')
    await rerender(<FreshnessNote freshness={stale(60)} />)
    await rerender(<FreshnessNote freshness={fresh()} />)
    expect(announcement(container)).toBe('')

    await rerender(<FreshnessNote freshness={offline(2)} />)
    expect(announcement(container)).toBe('离线 · 更新于 2 分钟前')
    await unmount()
  })
})

describe('发现页顶部的空媒体库说明（EmptyLibraryStrip）', () => {
  it('关掉之后焦点交给页面的主标题，不掉到 <body> 上', async () => {
    installLocalStorage()
    const { container, unmount } = await mount(
      <main>
        <h1>发现</h1>
        <EmptyLibraryStrip />
      </main>,
    )
    await act(async () => { await Promise.resolve() })
    const close = getByRole(container, 'button', { name: '关闭这条说明' })
    close.focus()
    await act(async () => close.click())
    expect(queryByRole(container, 'button', { name: '关闭这条说明' })).toBeNull()
    expect(document.activeElement).toBe(getByRole(container, 'heading', { level: 1, name: '发现' }))
    await unmount()
  })
})
