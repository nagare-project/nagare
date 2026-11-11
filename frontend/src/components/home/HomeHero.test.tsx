// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { HERO_INTERVAL_MS } from '../../lib/home/heroRotation'
import { heroSlide } from '../../lib/home/viewModels'
import { mount } from '../../test/harness'
import { getByRole } from '../../test/queries'
import { homeMediaFixture } from '../../test/discoverFixtures'
import { HomeHero } from './HomeHero'
import { HomeHueScope } from './HomeHueScope'

// 横幅的切换与暂停规则（DESIGN.md「动效」，WCAG 2.2.2）：每 8 秒换一张；鼠标在上面、键盘焦点在里面、
// 按了暂停时停住；手动切换后新的一张从头算 8 秒；减弱动态效果时不自己切、也没有暂停按钮。

const slides = [1, 2, 3].map((id) => heroSlide(homeMediaFixture({ anilistId: id, title: `作品 ${id}`, hue: id * 100, status: 'RELEASING' }), []))

let reduced = false
beforeEach(() => {
  reduced = false
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'performance'] })
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: reduced && query.includes('reduce'), addEventListener() {}, removeEventListener() {} }))
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function advance(ms: number) {
  await act(async () => { vi.advanceTimersByTime(ms) })
}
const bars = (root: HTMLElement) => [...root.querySelectorAll<HTMLButtonElement>('button[aria-pressed]')]
const current = (root: HTMLElement) => bars(root).findIndex((b) => b.getAttribute('aria-pressed') === 'true')

async function mountHero() {
  return mount(<HomeHueScope hues={slides.map((s) => s.hue)}><HomeHero slides={slides} /></HomeHueScope>)
}

describe('发现页横幅：8 秒切换与暂停规则', () => {
  it('每 8 秒换下一张，最后一张之后回到第一张；整页颜色跟着换', async () => {
    const { container, unmount } = await mountHero()
    const main = container.querySelector('main')!
    expect(current(container)).toBe(0)
    expect(main.style.getPropertyValue('--home-ground')).toBe('oklch(13% 0.012 100)')
    await advance(HERO_INTERVAL_MS)
    expect(current(container)).toBe(1)
    expect(main.style.getPropertyValue('--home-ground')).toBe('oklch(13% 0.012 200)')
    await advance(HERO_INTERVAL_MS)
    expect(current(container)).toBe(2)
    await advance(HERO_INTERVAL_MS)
    expect(current(container)).toBe(0)
    await unmount()
  })

  it('暂停按钮一直停到再按；名字说的是按下去会做什么', async () => {
    const { container, unmount } = await mountHero()
    const pause = getByRole(container, 'button', { name: '暂停自动切换' })
    await act(async () => pause.click())
    await advance(HERO_INTERVAL_MS * 3)
    expect(current(container)).toBe(0)
    await act(async () => getByRole(container, 'button', { name: '继续自动切换' }).click())
    await advance(HERO_INTERVAL_MS)
    expect(current(container)).toBe(1)
    await unmount()
  })

  it('鼠标在横幅上时停住，移开后接着剩下的时间走', async () => {
    const { container, unmount } = await mountHero()
    const hero = getByRole(container, 'region', { name: '本季焦点' })
    await advance(HERO_INTERVAL_MS - 2000)
    await act(async () => { hero.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })) })
    await advance(HERO_INTERVAL_MS * 2)
    expect(current(container)).toBe(0)
    await act(async () => { hero.dispatchEvent(new PointerEvent('pointerleave', { pointerType: 'mouse' })) })
    await advance(1900)
    expect(current(container)).toBe(0)
    await advance(200)
    expect(current(container)).toBe(1)
    await unmount()
  })

  it('点短条立刻换，新的一张从头算 8 秒；方向键在短条之间移动', async () => {
    const { container, unmount } = await mountHero()
    await advance(HERO_INTERVAL_MS - 1000)
    await act(async () => bars(container)[2]!.click())
    expect(current(container)).toBe(2)
    await advance(HERO_INTERVAL_MS - 100)
    expect(current(container)).toBe(2)
    await act(async () => {
      bars(container)[2]!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }))
    })
    expect(current(container)).toBe(1)
    expect(document.activeElement).toBe(bars(container)[1])
    await unmount()
  })

  it('减弱动态效果时不自己切，也不画暂停按钮', async () => {
    reduced = true
    const { container, unmount } = await mountHero()
    expect(container.querySelector('button:not([aria-pressed])')?.textContent ?? '').not.toContain('暂停')
    await advance(HERO_INTERVAL_MS * 3)
    expect(current(container)).toBe(0)
    await unmount()
  })

  it('后台刷新把前五部重新排了序：焦点跟着原来那一部走，不换成另一部', async () => {
    const view = (list: typeof slides) => <HomeHueScope hues={list.map((s) => s.hue)}><HomeHero slides={list} /></HomeHueScope>
    const { container, rerender, unmount } = await mount(view(slides))
    await act(async () => bars(container)[1]!.click())
    const visible = () => container.querySelector('article[aria-hidden="false"] h2')?.textContent
    expect(visible()).toContain('作品 2')
    await rerender(view([...slides].reverse()))
    expect(visible()).toContain('作品 2')
    expect(current(container)).toBe(1)
    await rerender(view([slides[1]!, slides[2]!, slides[0]!]))
    expect(visible()).toContain('作品 2')
    expect(current(container)).toBe(0)
    await unmount()
  })

  it('先挂上一个空横幅、数据后到：悬停停住照样生效', async () => {
    const view = (list: typeof slides) => <HomeHueScope hues={list.map((s) => s.hue)}><HomeHero slides={list} /></HomeHueScope>
    const { container, rerender, unmount } = await mount(view([]))
    expect(container.querySelector('section')).toBeNull()
    await rerender(view(slides))
    const hero = getByRole(container, 'region', { name: '本季焦点' })
    await act(async () => { hero.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })) })
    await advance(HERO_INTERVAL_MS * 2)
    expect(current(container)).toBe(0)
    await unmount()
  })

  it('只在手动切换时念：自己轮换不念', async () => {
    const { container, unmount } = await mountHero()
    const live = container.querySelector('[aria-live="polite"]')!
    await advance(HERO_INTERVAL_MS)
    expect(live.textContent).toBe('')
    await act(async () => bars(container)[2]!.click())
    expect(live.textContent).toBe('3 / 3：作品 3')
    await unmount()
  })

  it('只有一张时不轮换', async () => {
    const { container, unmount } = await mount(<HomeHueScope hues={[slides[0]!.hue]}><HomeHero slides={slides.slice(0, 1)} /></HomeHueScope>)
    expect(container.textContent).not.toContain('暂停自动切换')
    await advance(HERO_INTERVAL_MS * 2)
    expect(current(container)).toBe(0)
    await unmount()
  })
})
