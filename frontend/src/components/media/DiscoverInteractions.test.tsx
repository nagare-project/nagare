// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { DiscoverHero } from './DiscoverHero'
import { DiscoverCard } from './DiscoverCard'
import { DISCOVER_INTERVAL_MS } from './useDiscoverCarousel'
import type { MediaSummary } from './types'

const items: MediaSummary[] = [1, 2, 3].map(id => ({ id, title: `作品 ${id}`, episodes: 12, watched: 0, genres: ['奇幻'] }))
beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())
async function advance(ms: number) { await act(async () => { vi.advanceTimersByTime(ms) }) }
async function pointer(element: Element, type: string, relatedTarget: EventTarget | null = null) {
  await act(async () => { element.dispatchEvent(new MouseEvent(type, { bubbles: true, relatedTarget })) })
}

describe('Discover 鼠标与键盘交互', () => {
  it('鼠标点击横幅分页后，按钮留有焦点也会继续自动轮播', async () => {
    const { container, unmount } = await mount(<DiscoverHero items={items} />)
    const dots = [...container.querySelectorAll<HTMLButtonElement>('.hero-dot')]
    await pointer(dots[1]!, 'pointerdown')
    await act(async () => { dots[1]!.focus(); dots[1]!.click() })
    expect(document.activeElement).toBe(dots[1])
    expect(dots[1]?.getAttribute('aria-pressed')).toBe('true')
    await advance(DISCOVER_INTERVAL_MS)
    expect(dots[2]?.getAttribute('aria-pressed')).toBe('true')
    await unmount()
  })

  it('键盘焦点暂停横幅，移出后恢复；切到日程期间停播', async () => {
    const { container, rerender, unmount } = await mount(<DiscoverHero items={items} />)
    const dot = container.querySelector<HTMLButtonElement>('.hero-dot')!
    await act(async () => dot.focus())
    await advance(DISCOVER_INTERVAL_MS)
    expect(dot.getAttribute('aria-pressed')).toBe('true')
    await act(async () => dot.blur())
    await advance(DISCOVER_INTERVAL_MS)
    expect(dot.getAttribute('aria-pressed')).toBe('false')
    await rerender(<DiscoverHero items={items} showMetadata={false} />)
    await advance(DISCOVER_INTERVAL_MS * 2)
    await rerender(<DiscoverHero items={items} />)
    expect(container.querySelectorAll('.hero-dot')[1]?.getAttribute('aria-pressed')).toBe('true')
    await unmount()
  })

  it('快速移出再移入不会卸载卡片内容；鼠标点击后移出正常收起', async () => {
    const { container, unmount } = await mount(<DiscoverCard media={items[0]!} />)
    const card = container.querySelector('.discover-card')!
    await pointer(card, 'pointerover')
    const popup = container.querySelector('.discover-card-popup')!
    expect(popup.getAttribute('data-state')).toBe('open')
    const content = popup.firstChild
    await pointer(card, 'pointerout', document.body)
    await advance(20)
    await pointer(card, 'pointerover', document.body)
    await advance(20)
    expect(popup.firstChild).toBe(content)
    const cover = container.querySelector<HTMLButtonElement>('.discover-card-cover')!
    await pointer(cover, 'pointerdown')
    await act(async () => cover.focus())
    await pointer(card, 'pointerout', document.body)
    await advance(40)
    expect(popup.getAttribute('data-state')).toBe('closed')
    expect(popup.firstChild).toBeNull()
    await unmount()
  })
})
