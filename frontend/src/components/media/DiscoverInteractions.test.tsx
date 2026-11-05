// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { getByRole } from '../../test/queries'
import { DiscoverHero } from './DiscoverHero'
import { CARD_HOVER_INTENT_MS, DiscoverCard } from './DiscoverCard'
import { BANNER_INTERVAL_FALLBACK_MS } from './useBannerRotation'
import type { MediaSummary } from './types'

const items: MediaSummary[] = [1, 2, 3].map(id => ({ id, title: `作品 ${id}`, episodes: 12, watched: 0, genres: ['Fantasy'] }))
const INTERVAL = BANNER_INTERVAL_FALLBACK_MS
beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())
async function advance(ms: number) { await act(async () => { vi.advanceTimersByTime(ms) }) }
async function pointer(element: Element, type: string, relatedTarget: EventTarget | null = null) {
  await act(async () => { element.dispatchEvent(new MouseEvent(type, { bubbles: true, relatedTarget })) })
}
const dots = (container: HTMLElement) => [...container.querySelectorAll<HTMLButtonElement>('button')].filter(b => /^第 \d 部：/.test(b.getAttribute('aria-label') ?? ''))
const current = (container: HTMLElement) => dots(container).findIndex(dot => dot.getAttribute('aria-current') === 'true')
const heading = (container: HTMLElement) => container.querySelector('h2')?.textContent

describe('发现页横幅：8 秒切换与暂停规则', () => {
  it('每 8 秒换一部；鼠标点圆点切换后照常自动切（点过留下的焦点不算键盘焦点）', async () => {
    const { container, unmount } = await mount(<DiscoverHero items={items} label="本季新番" />)
    expect(heading(container)).toBe('作品 1')
    await advance(INTERVAL)
    expect(heading(container)).toBe('作品 2')
    const target = dots(container)[2]!
    await pointer(target, 'pointerdown')
    await act(async () => { target.focus(); target.click() })
    expect(current(container)).toBe(2)
    // 手动切换后新的那张从头算：差一点不切，到点才切
    await advance(INTERVAL - 10)
    expect(current(container)).toBe(2)
    await advance(10)
    expect(current(container)).toBe(0)
    await unmount()
  })

  it('键盘焦点停住横幅，移出后接着剩下的时间走', async () => {
    const { container, unmount } = await mount(<DiscoverHero items={items} label="本季新番" />)
    await advance(INTERVAL - 3000)
    const dot = dots(container)[0]!
    await act(async () => dot.focus())
    await advance(INTERVAL * 2)
    expect(current(container)).toBe(0)
    await act(async () => dot.blur())
    await advance(2990)
    expect(current(container)).toBe(0)
    await advance(10)
    expect(current(container)).toBe(1)
    await unmount()
  })

  it('鼠标悬停时停住，移开后接着走', async () => {
    const { container, unmount } = await mount(<DiscoverHero items={items} label="本季新番" />)
    const hero = container.querySelector('section')!
    await pointer(hero, 'pointerover')
    await advance(INTERVAL * 3)
    expect(current(container)).toBe(0)
    await pointer(hero, 'pointerout', document.body)
    await advance(INTERVAL)
    expect(current(container)).toBe(1)
    await unmount()
  })

  it('暂停按钮一直停到再按；按钮名字说的是按下去会做什么', async () => {
    const { container, unmount } = await mount(<DiscoverHero items={items} label="本季新番" />)
    const pause = [...container.querySelectorAll('button')].find(b => b.getAttribute('aria-label') === '暂停自动切换')!
    expect(pause).toBeDefined()
    await act(async () => pause.click())
    expect(pause.getAttribute('aria-label')).toBe('继续自动切换')
    await advance(INTERVAL * 3)
    expect(current(container)).toBe(0)
    await act(async () => pause.click())
    expect(pause.getAttribute('aria-label')).toBe('暂停自动切换')
    await advance(INTERVAL)
    expect(current(container)).toBe(1)
    await unmount()
  })

  it('只有一部时不画圆点和暂停按钮，也不起计时器', async () => {
    const { container, unmount } = await mount(<DiscoverHero items={items.slice(0, 1)} label="本季新番" />)
    expect(container.querySelectorAll('button')).toHaveLength(1) // 只有「预览」
    expect(vi.getTimerCount()).toBe(0)
    await unmount()
  })
})

describe('作品卡的悬停浮层', () => {
  it('鼠标停一会儿才弹出；快速移出再移入不会卸载内容；鼠标点击后移出正常收起', async () => {
    const { container, unmount } = await mount(<ul><DiscoverCard media={items[0]!} /></ul>)
    const card = container.querySelector('li')!
    const popup = container.querySelector('[data-state]')!
    await pointer(card, 'pointerover')
    expect(popup.getAttribute('data-state')).toBe('closed')
    await advance(CARD_HOVER_INTENT_MS)
    expect(popup.getAttribute('data-state')).toBe('open')
    const content = popup.firstChild
    await pointer(card, 'pointerout', document.body)
    await advance(20)
    await pointer(card, 'pointerover', document.body)
    await advance(CARD_HOVER_INTENT_MS)
    expect(popup.firstChild).toBe(content)
    const link = container.querySelector<HTMLAnchorElement>('a[href="/entry?id=1"]')!
    await pointer(link, 'pointerdown')
    await act(async () => link.focus())
    await pointer(card, 'pointerout', document.body)
    await advance(40)
    expect(popup.getAttribute('data-state')).toBe('closed')
    expect(popup.firstChild).toBeNull()
    await unmount()
  })

  it('按 Esc 收起浮层（WCAG 1.4.13）：鼠标不用移开；移开再停回来才重新弹出', async () => {
    const { container, unmount } = await mount(<ul><DiscoverCard media={items[0]!} /></ul>)
    const card = container.querySelector('li')!
    const popup = container.querySelector('[data-state]')!
    await pointer(card, 'pointerover')
    await advance(CARD_HOVER_INTENT_MS)
    expect(popup.getAttribute('data-state')).toBe('open')
    await act(async () => { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })) })
    expect(popup.getAttribute('data-state')).toBe('closed')
    // 鼠标还停在卡片上：不会自己又弹出来
    await advance(CARD_HOVER_INTENT_MS * 3)
    expect(popup.getAttribute('data-state')).toBe('closed')
    await pointer(card, 'pointerout', document.body)
    await pointer(card, 'pointerover', document.body)
    await advance(CARD_HOVER_INTENT_MS)
    expect(popup.getAttribute('data-state')).toBe('open')
    await unmount()
  })

  it('键盘焦点在浮层里时按 Esc：浮层收起，焦点回到卡片链接（不掉进藏起来的浮层或 <body>）', async () => {
    const { container, unmount } = await mount(<ul><DiscoverCard media={items[0]!} /></ul>)
    const link = getByRole(container, 'link', { name: /^作品 1/ })
    await act(async () => { link.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true })); link.focus() })
    const popup = container.querySelector('[data-state]')!
    expect(popup.getAttribute('data-state')).toBe('open')
    const play = getByRole(container, 'button', { name: /^播放 / })
    await act(async () => play.focus())
    await act(async () => { play.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })) })
    expect(popup.getAttribute('data-state')).toBe('closed')
    expect(document.activeElement).toBe(link)
    // 焦点离开这张卡之后，下次再进来照常弹出
    await act(async () => link.blur())
    await act(async () => { link.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true })); link.focus() })
    expect(popup.getAttribute('data-state')).toBe('open')
    await unmount()
  })

  it('整张卡是一个链接：名字是标题 + 副标题，不另写 aria-label（语音控制念可见文字就能点到）', async () => {
    const media: MediaSummary = { ...items[0]!, year: 2026, season: 'FALL', status: 'RELEASING' }
    const { container, unmount } = await mount(<ul><DiscoverCard media={media} /></ul>)
    const link = container.querySelector<HTMLAnchorElement>('a[href="/entry?id=1"]')!
    expect(link.hasAttribute('aria-label')).toBe(false)
    expect(link.textContent).toContain('作品 1')
    expect(link.textContent).toContain('2026 秋 · 12 集')
    // 封面角标只是视觉，同样的信息以文字放在链接里
    expect(link.querySelector('[aria-hidden="true"]')?.textContent).toContain('放送中')
    expect(link.querySelector('.visually-hidden')?.textContent).toBe('放送中')
    await unmount()
  })

  it('海报色写在卡片自己身上；没有色阶时不写，落回中性值', async () => {
    const tones = { hue: 250, tone: '#88b6e6', quiet: '#779cc2', mid: '#496c90', low: '#0d2034', onTone: '#030c17' }
    const { container, unmount } = await mount(<ul><DiscoverCard media={{ ...items[0]!, posterTones: tones }} /><DiscoverCard media={{ ...items[1]!, posterTones: null }} /></ul>)
    const [toned, neutral] = [...container.querySelectorAll('li')]
    expect(toned!.style.getPropertyValue('--poster-tone')).toBe('#88b6e6')
    expect(toned!.style.getPropertyValue('--poster-tone-mid')).toBe('#496c90')
    expect(neutral!.getAttribute('style')).toBeNull()
    await unmount()
  })

  it('评分为 0 或缺席时浮层里不出评分，而不是显示一个 0 分', async () => {
    const { container, unmount } = await mount(<ul><DiscoverCard media={{ ...items[0]!, score: 0 }} /><DiscoverCard media={{ ...items[1]!, score: 82 }} /></ul>)
    for (const link of container.querySelectorAll<HTMLAnchorElement>('li > a')) {
      await act(async () => link.focus())
    }
    const scores = [...container.querySelectorAll('.poster-card-score')].map(e => e.textContent)
    expect(scores).toEqual(['评分 8.2'])
    await unmount()
  })
})
