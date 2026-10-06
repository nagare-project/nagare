// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { CarouselRow, MAX_ROW_DOTS } from './CarouselRow'

// jsdom 没有布局，Embla 算不出吸附点；这里直接给一个有 N 个吸附点的 api 替身，
// 只验证「几个吸附点 → 画什么控件」这一层。
// api 必须是同一个对象：组件的 effect 依赖它，每次渲染给一个新对象就会无限重跑。
const { embla } = vi.hoisted(() => {
  const state = { snapCount: 0 }
  const root = { addEventListener: () => {}, removeEventListener: () => {} }
  const api: Record<string, unknown> = {
    scrollSnapList: () => Array.from({ length: state.snapCount }, (_, i) => i),
    selectedScrollSnap: () => 0,
    canScrollPrev: () => false,
    canScrollNext: () => state.snapCount > 1,
    rootNode: () => root,
    scrollPrev: () => {},
    scrollNext: () => {},
    scrollTo: () => {},
  }
  api.on = () => api
  api.off = () => api
  const viewportRef = () => {}
  return { embla: { state, api, viewportRef } }
})
vi.mock('embla-carousel-react', () => ({ default: () => [embla.viewportRef, embla.api] }))
vi.mock('embla-carousel-autoplay', () => ({ default: () => ({ play: () => {}, stop: () => {} }) }))

async function render(count: number) {
  embla.state.snapCount = count
  return mount(<CarouselRow title="热门" autoPlay>{Array.from({ length: count }, (_, i) => <li key={i}>卡片 {i}</li>)}</CarouselRow>)
}

describe('CarouselRow 的分页控件', () => {
  it('吸附点不超过上限时逐个画分页点，并备一对箭头给窄屏（CSS 按宽度二选一）', async () => {
    const { container, unmount } = await render(MAX_ROW_DOTS)
    expect(container.querySelectorAll('.row-dot')).toHaveLength(MAX_ROW_DOTS)
    expect(container.querySelectorAll('.row-arrow')).toHaveLength(2)
    await unmount()
  })

  it('超过上限改画前后翻页箭头，不能什么都不画（单指点按的用户没法拖拽）', async () => {
    const { container, unmount } = await render(MAX_ROW_DOTS + 1)
    expect(container.querySelectorAll('.row-dot')).toHaveLength(0)
    expect(container.querySelector('[aria-label="热门向前翻页"]')).not.toBeNull()
    expect(container.querySelector('[aria-label="热门向后翻页"]')).not.toBeNull()
    await unmount()
  })

  it('只有一页时不画孤零零的一个点，也不画箭头和暂停', async () => {
    const { container, unmount } = await render(1)
    expect(container.querySelectorAll('.row-dot')).toHaveLength(0)
    expect(container.querySelectorAll('.row-arrow')).toHaveLength(0)
    expect(container.querySelector('.row-pause')).toBeNull()
    await unmount()
  })

  it('能翻页就有暂停按钮，且不和分页点绑在同一个元素上（窄屏只藏点、留暂停）', async () => {
    const { container, unmount } = await render(5)
    const pause = container.querySelector('.row-pause')
    expect(pause).not.toBeNull()
    expect(pause!.classList.contains('row-dot')).toBe(false)
    await unmount()
  })
})
