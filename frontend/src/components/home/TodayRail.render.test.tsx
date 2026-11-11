import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { hhmm } from '../../lib/home/time'
import type { TodayCard } from '../../lib/home/viewModels'
import { TodayRail, type TodayRailTomorrow } from './TodayRail'

// 用 renderToStaticMarkup（不需要 jsdom）。这一排的「现在」来自 useHomeClock，即系统时钟：
// 在两个时刻各渲染一次，就是时钟走过了一集的播出时间。
//
// 钉住的是：当天的每一部不管几点都是这一排里的一张卡，一部作品播出时是换状态、不是离开；
// 这一排用一个有名字的滑块移动，不是按钮；末尾说今天的就这些、明天有几部、完整放送表在哪。
// （CSS 那一半 —— 曾经在手机上藏起已播的卡 —— 在 TodayRail.css.test.ts。）

const MIN = 60_000
const NOW = Date.UTC(2026, 9, 7, 12, 0)

const item = (key: number, offsetMin: number): TodayCard => ({
  id: 9_000 + key,
  title: `测试番 ${key}`,
  cover: null,
  hue: (key * 70) % 360,
  key,
  at: NOW + offsetMin * MIN,
  ep: key + 1,
})

// 两小时前、半小时前已播；2 分钟后、45 分钟后、3 小时后
const ITEMS = [item(1, -120), item(2, -30), item(3, 2), item(4, 45), item(5, 180)]
const TOMORROW: TodayRailTomorrow = { label: '周四 10月8日', count: 27 }

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

function render(nowMs: number, items = ITEMS, tomorrow: TodayRailTomorrow | null = TOMORROW): string {
  vi.setSystemTime(nowMs)
  return renderToStaticMarkup(<TodayRail items={items} dayLabel="周三 10月7日" tomorrow={tomorrow} />)
}

/** 一排末尾的面板：从它的标题到这一排结束 */
function endOf(html: string): string {
  const at = html.indexOf('今天的番就这些')
  return at < 0 ? '' : html.slice(at)
}

/** 每张卡的 data-state，按排列顺序 */
function states(html: string): string[] {
  return [...html.matchAll(/<a\b[^>]*>/g)]
    .map((m) => m[0])
    .filter((tag) => /href="\/entry\?id=9\d{3}"/.test(tag))
    .map((tag) => /data-state="(\w+)"/.exec(tag)?.[1] ?? 'none')
}

describe('TodayRail', () => {
  test('当天的每一部都是自己的一张卡', () => {
    const html = render(NOW)
    expect(html.match(/href="\/entry\?id=9\d{3}"/g)).toHaveLength(ITEMS.length)
    expect(states(html)).toEqual(['aired', 'aired', 'soon', 'soon', 'later'])
    // 没有折叠：没什么可展开，也没有收起的
    expect(html).not.toContain('aria-expanded')
  })

  test('页面开着时播出的那一部留在这一排里、变成已播', () => {
    const later = render(NOW + 3 * MIN)
    expect(later.match(/href="\/entry\?id=9\d{3}"/g)).toHaveLength(ITEMS.length)
    expect(states(later)).toEqual(['aired', 'aired', 'aired', 'soon', 'later'])
    expect(later.split('已播').length - 1).toBe(3)
  })

  test('「现在」标记在已播与未播之间', () => {
    const html = render(NOW)
    const marker = html.indexOf('data-now')
    expect(marker).toBeGreaterThan(html.indexOf('href="/entry?id=9002"'))
    expect(marker).toBeLessThan(html.indexOf('href="/entry?id=9003"'))
  })

  test('一小时内播出的倒数分钟', () => {
    const html = render(NOW)
    expect(html).toContain('2 分钟后')
    expect(html).toContain('45 分钟后')
  })

  test('每张卡带着机器可读的 <time>', () => {
    const html = render(NOW)
    const at = NOW + 2 * MIN
    expect(html).toContain(`<time dateTime="${new Date(at).toISOString()}">${hhmm(at)}</time>`)
  })

  test('没有 ← → 按钮：这一排由一个说明自己做什么、控制这一排的滑块移动', () => {
    const html = render(NOW)
    expect(html).not.toContain('<button')
    const input = /<input\b[^>]*type="range"[^>]*>/.exec(html)?.[0] ?? ''
    expect(input).toContain('aria-label="横向滚动今日更新"')
    expect(input).toContain('aria-controls="home-today-rail"')
    expect(html).toContain('id="home-today-rail"')
  })

  test('量出来之前滑块占着位置但什么都不画，也不是 Tab 的一站', () => {
    const html = render(NOW)
    expect(html).toContain('data-overflow="false"')
    expect(/<input\b[^>]*type="range"[^>]*>/.exec(html)?.[0]).toContain('disabled')
  })

  test('一排的末尾说今天的就这些、明天有几部、完整放送表在哪', () => {
    const end = endOf(render(NOW))
    expect(end).toContain('明天 · 周四 10月8日 · 27 部')
    expect(end).toContain('href="/schedule"')
    expect(end).toContain('看完整放送表')
    // 在所有卡之后：面板不是作品，不算一张卡
    expect(end).not.toContain('href="/entry')
    expect(end).not.toContain('data-state')
  })

  test('明天没有番时只说是哪天；没有明天就没有这一行', () => {
    expect(endOf(render(NOW, ITEMS, { label: '周四 10月8日', count: 0 }))).toContain('明天 · 周四 10月8日<')
    const none = endOf(render(NOW, ITEMS, null))
    expect(none).toContain('看完整放送表')
    expect(none).not.toContain('明天')
  })

  test('全部播完时末尾面板在「现在」标记之后', () => {
    const html = render(NOW + 4 * 60 * MIN)
    expect(states(html)).toEqual(['aired', 'aired', 'aired', 'aired', 'aired'])
    expect(html.indexOf('data-now')).toBeGreaterThan(html.indexOf('href="/entry?id=9005"'))
    expect(html.indexOf('今天的番就这些')).toBeGreaterThan(html.indexOf('data-now'))
  })

  test('没有番的一天就这么说，没有横排也没有滑块', () => {
    const html = render(NOW, [])
    expect(html).toContain('今日暂无更新')
    expect(html).not.toContain('home-today-rail')
    expect(html).not.toContain('type="range"')
  })
})
