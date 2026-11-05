// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { getByRole, queryByRole } from '../../test/queries'
import { DiscoverHero } from './DiscoverHero'
import { BANNER_INTERVAL_FALLBACK_MS } from './useBannerRotation'
import type { MediaSummary } from './types'

vi.mock('../../lib/media', async (importOriginal) => ({ ...await importOriginal<typeof import('../../lib/media')>(), fetchMedia: vi.fn(async () => { throw new Error('离线') }) }))
vi.mock('../../lib/endpoints', async (importOriginal) => ({ ...await importOriginal<typeof import('../../lib/endpoints')>(), fetchLibrary: vi.fn(async () => ({ clusters: [], folders: [], scannedAt: null, continueWatching: [] })) }))

const work = (id: number): MediaSummary => ({ id, title: `作品 ${id}`, episodes: 12, watched: 0, genres: [] })
const INTERVAL = BANNER_INTERVAL_FALLBACK_MS
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')

beforeEach(() => {
  vi.useFakeTimers()
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function (this: HTMLDialogElement) { this.setAttribute('open', '') } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: function (this: HTMLDialogElement) {
    this.removeAttribute('open')
    this.dispatchEvent(new Event('close', { bubbles: true }))
  } })
})
afterEach(() => {
  vi.useRealTimers()
  for (const [name, descriptor] of [['showModal', show], ['close', close]] as const) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, name, descriptor)
    else Reflect.deleteProperty(HTMLDialogElement.prototype, name)
  }
})

async function advance(ms: number) {
  await act(async () => { vi.advanceTimersByTime(ms) })
}
const slideTitle = (container: HTMLElement) => container.querySelector('.hero-title')?.textContent

describe('发现页横幅：预览开着时后台刷新换了数据', () => {
  it('对话框不会被卸掉、横幅不换；关上之后焦点回到「预览」，移走之后换成新数据并接着自动切换（不会一直停住）', async () => {
    const { container, rerender, unmount } = await mount(<DiscoverHero items={[work(1), work(2), work(3)]} label="本季新番" />)
    await act(async () => getByRole(container, 'button', { name: '预览 作品 1' }).click())
    expect(getByRole(container, 'dialog')).toBeDefined()

    // 后台刷新：整组作品都换了（顺序与内容都不同）
    await rerender(<DiscoverHero items={[work(7), work(8)]} label="本季新番" />)
    expect(getByRole(container, 'dialog')).toBeDefined()
    expect(slideTitle(container)).toBe('作品 1')
    await advance(INTERVAL * 2)
    expect(slideTitle(container)).toBe('作品 1')

    // 关上：焦点回到原来那个「预览」按钮（它还在，没有随换数据被卸掉）
    await act(async () => getByRole(container, 'button', { name: '关闭预览' }).click())
    expect(queryByRole(container, 'dialog')).toBeNull()
    expect(document.activeElement).toBe(getByRole(container, 'button', { name: '预览 作品 1' }))
    expect(slideTitle(container)).toBe('作品 1')

    // 焦点离开横幅：换成新数据，计时照常走
    await act(async () => (document.activeElement as HTMLElement).blur())
    expect(slideTitle(container)).toBe('作品 7')
    await advance(INTERVAL)
    expect(slideTitle(container)).toBe('作品 8')
    await unmount()
  })

  it('鼠标停在横幅上时后台换了数据：先不换，移开之后再换', async () => {
    const { container, rerender, unmount } = await mount(<DiscoverHero items={[work(1), work(2)]} label="本季新番" />)
    const hero = getByRole(container, 'region', { name: '精选作品' })
    await act(async () => { hero.dispatchEvent(new MouseEvent('pointerover', { bubbles: true })) })
    await rerender(<DiscoverHero items={[work(9)]} label="本季新番" />)
    expect(slideTitle(container)).toBe('作品 1')
    await act(async () => { hero.dispatchEvent(new MouseEvent('pointerout', { bubbles: true, relatedTarget: document.body })) })
    expect(slideTitle(container)).toBe('作品 9')
    await unmount()
  })
})
