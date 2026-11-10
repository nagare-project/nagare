// @vitest-environment jsdom
import { act, useContext } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { installFakeIntersectionObserver } from '../../test/intersectionObserver'
import type { FakeIntersectionObservers } from '../../test/intersectionObserver'
import { ArtworkLoadingContext, resetNearViewportForTests } from './artworkLoading'
import { CarouselRow, PAGES_AHEAD, ROW_NEAR_MARGIN, preloadEnd } from './CarouselRow'

// Embla 替身：能报「屏上看到哪几张」（slidesInView）、能不能往两头滚，并记下组件传进来的选项
const { embla } = vi.hoisted(() => {
  const state = { inView: [] as number[], overflow: true, options: undefined as Record<string, unknown> | undefined, listeners: new Map<string, Set<() => void>>() }
  const root = { addEventListener: () => {}, removeEventListener: () => {} }
  const api: Record<string, unknown> = {
    scrollSnapList: () => [0, 1, 2],
    selectedScrollSnap: () => 0,
    canScrollPrev: () => false,
    canScrollNext: () => state.overflow,
    rootNode: () => root,
    slidesInView: () => state.inView,
  }
  api.on = (event: string, listener: () => void) => {
    state.listeners.set(event, (state.listeners.get(event) ?? new Set()).add(listener))
    return api
  }
  api.off = (event: string, listener: () => void) => {
    state.listeners.get(event)?.delete(listener)
    return api
  }
  const emit = (event: string) => { for (const listener of state.listeners.get(event) ?? []) listener() }
  return { embla: { state, api, emit } }
})
vi.mock('embla-carousel-react', () => ({
  default: (options: Record<string, unknown>) => {
    embla.state.options = options
    return [() => {}, embla.api]
  },
}))

/** 读出自己拿到的加载档，画在 data-loading 上 */
function Slide({ index }: { index: number }) {
  const loading = useContext(ArtworkLoadingContext)
  return <li className="poster-card" data-index={index} data-loading={loading ?? 'none'} />
}

const COUNT = 40
const loadingOf = (container: HTMLElement) => [...container.querySelectorAll<HTMLElement>('[data-index]')].map((li) => li.dataset.loading)
const eagerCount = (container: HTMLElement) => loadingOf(container).filter((value) => value === 'eager').length

let io: FakeIntersectionObservers
beforeEach(() => {
  resetNearViewportForTests()
  io = installFakeIntersectionObserver()
  embla.state.inView = []
  embla.state.overflow = true
  embla.state.listeners.clear()
})
afterEach(() => {
  resetNearViewportForTests()
  io.restore()
})

async function render() {
  return mount(<CarouselRow title="本季新番">{Array.from({ length: COUNT }, (_, i) => <Slide key={i} index={i} />)}</CarouselRow>)
}

describe('海报行的封面加载窗口', () => {
  it('窗口右端 = 屏上最后一张 + 往后 PAGES_AHEAD 页；Embla 还没量出来时按一页 6 张算', () => {
    expect(PAGES_AHEAD).toBe(2)
    expect(preloadEnd([])).toBe(18)
    // 一屏 6 张 + 露出一角的第 7 张：一页按 7 张算
    expect(preloadEnd([0, 1, 2, 3, 4, 5, 6])).toBe(7 + 14)
    expect(preloadEnd([12, 13, 14, 15, 16, 17])).toBe(18 + 12)
  })

  it('行还没到视口附近时一张都不提前加载；到了附近（上下 1.5 屏）才按窗口加载', async () => {
    const { container, unmount } = await render()
    expect(loadingOf(container).every((value) => value === 'lazy')).toBe(true)
    const section = container.querySelector('section')!
    expect(io.observers.find((record) => record.targets.has(section))?.options?.rootMargin).toBe(ROW_NEAR_MARGIN)
    await act(async () => io.intersect(section))
    expect(eagerCount(container)).toBe(18)
    expect(loadingOf(container).slice(0, 18).every((value) => value === 'eager')).toBe(true)
    await unmount()
  })

  it('往后翻时窗口跟着屏上看到的卡往后扩；翻回来也不收回', async () => {
    const { container, unmount } = await render()
    await act(async () => io.intersect(container.querySelector('section')!))
    embla.state.inView = [0, 1, 2, 3, 4, 5, 6]
    await act(async () => embla.emit('slidesInView'))
    expect(eagerCount(container)).toBe(21)
    embla.state.inView = [7, 8, 9, 10, 11, 12, 13]
    await act(async () => embla.emit('slidesInView'))
    expect(eagerCount(container)).toBe(28)
    embla.state.inView = [0, 1, 2, 3, 4, 5, 6]
    await act(async () => embla.emit('slidesInView'))
    expect(eagerCount(container)).toBe(28)
    await unmount()
  })

  it('加载档只是 context，不加 DOM：卡片仍是 ul 的直接子元素', async () => {
    const { container, unmount } = await render()
    const track = container.querySelector('.discover-row-track')!
    expect(track.children).toHaveLength(COUNT)
    expect([...track.children].every((child) => child.matches('li.poster-card'))).toBe(true)
    await unmount()
  })
})

describe('海报行的拖拽', () => {
  const watchDrag = () => embla.state.options?.watchDrag as (api: unknown, event: { target: EventTarget | null }) => boolean

  it('一屏放得下（两头都滚不动）的行不能拖：否则卡片能被拽出去一大截再弹回来', async () => {
    const { unmount } = await render()
    embla.state.overflow = false
    expect(watchDrag()(embla.api, { target: document.body })).toBe(false)
    embla.state.overflow = true
    expect(watchDrag()(embla.api, { target: document.body })).toBe(true)
    await unmount()
  })

  it('卡片浮层里开着的对话框不能当拖拽起点', async () => {
    const { unmount } = await render()
    const dialog = document.createElement('dialog')
    const button = document.createElement('button')
    dialog.appendChild(button)
    document.body.appendChild(dialog)
    expect(watchDrag()(embla.api, { target: button })).toBe(false)
    dialog.remove()
    await unmount()
  })
})
