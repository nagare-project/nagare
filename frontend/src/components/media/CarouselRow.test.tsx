// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { CarouselRow, MAX_ROW_DOTS } from './CarouselRow'

// jsdom 没有布局，Embla 算不出吸附点；这里直接给一个有 N 页的 api 替身，
// 只验证「几页 → 画什么控件」与按钮接到了哪个 api 调用这一层。
// 翻页像 Embla 一样同步改当前页、同步发 select 事件（组件靠它更新箭头与分页点）。
// api 必须是同一个对象：组件的 effect 依赖它，每次渲染给一个新对象就会无限重跑。
const { embla } = vi.hoisted(() => {
  const state = { snapCount: 0, selected: 0, calls: [] as string[], listeners: new Map<string, Set<() => void>>() }
  const root = { addEventListener: () => {}, removeEventListener: () => {} }
  const select = (index: number) => {
    state.selected = Math.max(0, Math.min(state.snapCount - 1, index))
    for (const listener of state.listeners.get('select') ?? []) listener()
  }
  const api: Record<string, unknown> = {
    scrollSnapList: () => Array.from({ length: state.snapCount }, (_, i) => i),
    selectedScrollSnap: () => state.selected,
    canScrollPrev: () => state.selected > 0,
    canScrollNext: () => state.selected < state.snapCount - 1,
    rootNode: () => root,
    scrollPrev: () => { state.calls.push('prev'); select(state.selected - 1) },
    scrollNext: () => { state.calls.push('next'); select(state.selected + 1) },
    scrollTo: (index: number) => { state.calls.push(`to:${index}`); select(index) },
  }
  api.on = (event: string, listener: () => void) => {
    state.listeners.set(event, (state.listeners.get(event) ?? new Set()).add(listener))
    return api
  }
  api.off = (event: string, listener: () => void) => {
    state.listeners.get(event)?.delete(listener)
    return api
  }
  const viewportRef = () => {}
  return { embla: { state, api, viewportRef } }
})
vi.mock('embla-carousel-react', () => ({ default: () => [embla.viewportRef, embla.api] }))

async function render(count: number) {
  embla.state.snapCount = count
  embla.state.selected = 0
  embla.state.calls = []
  return mount(<CarouselRow title="热门">{Array.from({ length: count }, (_, i) => <li key={i}>卡片 {i}</li>)}</CarouselRow>)
}

const buttons = (container: HTMLElement, name: RegExp) => [...container.querySelectorAll('button')].filter((b) => name.test(b.getAttribute('aria-label') ?? ''))

describe('海报行的翻页控件', () => {
  it('页数不超过上限时逐页画分页点，并且一直带一对前后箭头（拖拽对语音控制用户不可用）', async () => {
    const { container, unmount } = await render(MAX_ROW_DOTS)
    expect(buttons(container, /^热门第 \d+ 页$/)).toHaveLength(MAX_ROW_DOTS)
    expect(buttons(container, /^热门向[前后]翻页$/)).toHaveLength(2)
    expect(container.querySelector('[role="group"]')?.getAttribute('aria-label')).toBe('热门分页')
    await unmount()
  })

  it('超过上限只画前后箭头，不画几十个点', async () => {
    const { container, unmount } = await render(MAX_ROW_DOTS + 1)
    expect(buttons(container, /^热门第 \d+ 页$/)).toHaveLength(0)
    expect(buttons(container, /^热门向前翻页$/)).toHaveLength(1)
    expect(buttons(container, /^热门向后翻页$/)).toHaveLength(1)
    await unmount()
  })

  it('只有一页时不画孤零零的一个点，也不画箭头', async () => {
    const { container, unmount } = await render(1)
    expect(container.querySelectorAll('button')).toHaveLength(0)
    await unmount()
  })

  it('海报行不自动滚动：没有暂停按钮，挂着不动也不会自己翻页', async () => {
    vi.useFakeTimers()
    try {
      const { container, unmount } = await render(5)
      expect(buttons(container, /暂停|播放/)).toHaveLength(0)
      await act(async () => { vi.advanceTimersByTime(60_000) })
      expect(embla.state.calls).toEqual([])
      await unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('箭头与分页点接到 Embla；当前页标 aria-current，到头的箭头不可点', async () => {
    const { container, unmount } = await render(3)
    const [prev] = buttons(container, /^热门向前翻页$/)
    const [next] = buttons(container, /^热门向后翻页$/)
    expect(prev?.disabled).toBe(true)
    expect(next?.disabled).toBe(false)
    await act(async () => next!.click())
    const dots = buttons(container, /^热门第 \d+ 页$/)
    expect(dots[1]?.getAttribute('aria-current')).toBe('true')
    await act(async () => dots[2]!.click())
    expect(embla.state.calls).toEqual(['next', 'to:2'])
    expect(dots[2]?.getAttribute('aria-current')).toBe('true')
    expect(dots[0]?.hasAttribute('aria-current')).toBe(false)
    expect(next?.disabled).toBe(true)
    await unmount()
  })

  it('用箭头翻到头：那个箭头变成不可点之前，焦点交给另一头的箭头（不掉到 <body> 上）', async () => {
    const { container, unmount } = await render(3)
    const [prev] = buttons(container, /^热门向前翻页$/)
    const [next] = buttons(container, /^热门向后翻页$/)
    next!.focus()
    await act(async () => next!.click())
    expect(document.activeElement).toBe(next)
    await act(async () => next!.click())
    expect(next?.disabled).toBe(true)
    expect(document.activeElement).toBe(prev)
    // 往回翻到头也一样
    await act(async () => prev!.click())
    await act(async () => prev!.click())
    expect(prev?.disabled).toBe(true)
    expect(document.activeElement).toBe(next)
    await unmount()
  })

  it('行用标题命名（section + aria-labelledby），标题是二级标题', async () => {
    const { container, unmount } = await render(2)
    const section = container.querySelector('section')!
    const heading = container.querySelector('h2')!
    expect(heading.textContent).toBe('热门')
    expect(section.getAttribute('aria-labelledby')).toBe(heading.id)
    await unmount()
  })
})
