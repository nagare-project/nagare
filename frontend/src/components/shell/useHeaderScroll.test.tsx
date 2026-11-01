// @vitest-environment jsdom
import { act, useRef } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { useHeaderScroll } from './useHeaderScroll'
import type { HeaderScrollState } from './useHeaderScroll'

let latest: HeaderScrollState = { scrolled: false, hidden: false, instant: false }

function Probe({ locked }: { locked: boolean }) {
  const header = useRef<HTMLElement>(null)
  latest = useHeaderScroll(header, locked)
  return <header ref={header}><a href="/">字标</a></header>
}

async function scrollTo(y: number): Promise<void> {
  await act(async () => {
    Object.defineProperty(window, 'scrollY', { configurable: true, value: y })
    window.dispatchEvent(new Event('scroll'))
  })
}

beforeEach(() => {
  Object.defineProperty(window, 'scrollY', { configurable: true, value: 0 })
  // 每帧最多算一次：测试里让 rAF 同步执行并返回 0（「没有待执行的帧」），下一次滚动照常排进来
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return 0 })
  vi.stubGlobal('cancelAnimationFrame', () => {})
})
afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useHeaderScroll', () => {
  it('滚过 8px 出现玻璃；往下连续 12px 收起，往上连续 8px 滑出', async () => {
    const view = await mount(<Probe locked={false} />)
    expect(latest).toMatchObject({ scrolled: false, hidden: false })

    await scrollTo(8)
    expect(latest.scrolled).toBe(false)
    await scrollTo(9)
    expect(latest).toMatchObject({ scrolled: true, hidden: false })

    await scrollTo(11) // 从顶部连续往下 11px：还不够
    expect(latest.hidden).toBe(false)
    await scrollTo(12) // 连续往下 12px
    expect(latest.hidden).toBe(true)

    await scrollTo(200)
    await scrollTo(194) // 往上 6px：还不够
    expect(latest.hidden).toBe(true)
    await scrollTo(192) // 往上累计 8px
    expect(latest.hidden).toBe(false)

    await scrollTo(203) // 方向一换就重新累计：往下 11px 不收
    expect(latest.hidden).toBe(false)
    await scrollTo(204)
    expect(latest.hidden).toBe(true)

    await scrollTo(0)
    expect(latest).toMatchObject({ scrolled: false, hidden: false })
    await view.unmount()
  })

  it('抽屉 / 搜索打开（locked）时不收起，已收起的立即出现', async () => {
    const view = await mount(<Probe locked={false} />)
    await scrollTo(100)
    expect(latest.hidden).toBe(true)

    await view.rerender(<Probe locked />)
    expect(latest.hidden).toBe(false)
    await scrollTo(300)
    expect(latest.hidden).toBe(false)
    await view.unmount()
  })

  it('键盘焦点落进已收起的顶栏：立刻出现、不滑', async () => {
    const view = await mount(<Probe locked={false} />)
    await scrollTo(100)
    expect(latest.hidden).toBe(true)

    await act(async () => { view.container.querySelector('a')!.focus() })
    expect(latest).toMatchObject({ hidden: false, instant: true })
    // 焦点还在顶栏里时继续往下滚也不收起
    await scrollTo(400)
    expect(latest.hidden).toBe(false)
    await view.unmount()
  })
})
