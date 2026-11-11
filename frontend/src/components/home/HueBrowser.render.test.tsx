import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import type { HueCard } from '../../lib/home/viewModels'
import { HueBrowser, type HueFamilyView } from './HueBrowser'

// 钉住的是：一组的面板放着这一组的全部作品（不是前几部）；胶囊上的数就是它后面的卡数；
// 每一排有以这一组命名的滑块；末尾提供下一组；只有正在显示的那一组加载封面、对读屏可见。

const card = (id: number, hue: number): HueCard => ({ id, title: `测试番 ${id}`, cover: `/art/cap/remote/${id}`, hue, score: '7.5', meta: 'TV · 连载中' })

const family = (key: HueFamilyView['key'], label: string, hue: number, n: number, base: number): HueFamilyView => ({
  key, label, hue, items: Array.from({ length: n }, (_, i) => card(base + i, hue)),
})

const BLUE = family('blue', '蓝调', 245, 15, 1000)
const PURPLE = family('purple', '紫调', 305, 3, 2000)
const RED = family('red', '红调', 20, 8, 3000)

function render(families: HueFamilyView[], defaultKey = families[0]?.key ?? 'blue'): string {
  return renderToStaticMarkup(<HueBrowser total={26} families={families} defaultKey={defaultKey} />)
}

/** 一组的面板：从它的横排 id 到下一个面板（或结束） */
function panelOf(html: string, key: string): string {
  const at = html.indexOf(`id="home-hue-${key}"`)
  if (at < 0) return ''
  const rest = html.slice(at + 1)
  const next = rest.search(/id="home-hue-(?:red|orange|yellow|green|cyan|blue|purple)"/)
  return next < 0 ? rest : rest.slice(0, next)
}

const cardsIn = (panel: string) => panel.match(/href="\/entry\?id=\d+"/g) ?? []

describe('HueBrowser', () => {
  test('一组的面板放着这一组的每一部，不是前十部', () => {
    const html = render([RED, BLUE, PURPLE], 'blue')
    expect(cardsIn(panelOf(html, 'blue'))).toHaveLength(15)
    expect(cardsIn(panelOf(html, 'red'))).toHaveLength(8)
    expect(cardsIn(panelOf(html, 'purple'))).toHaveLength(3)
  })

  test('胶囊上的数就是它后面的卡数', () => {
    const html = render([RED, BLUE, PURPLE], 'blue')
    expect(html).toMatch(/蓝调<span[^>]*>15 部<\/span>/)
    expect(html).toMatch(/红调<span[^>]*>8 部<\/span>/)
  })

  test('每一排由以这一组命名的滑块移动', () => {
    const html = render([RED, BLUE, PURPLE], 'blue')
    for (const [key, label] of [['red', '红调'], ['blue', '蓝调'], ['purple', '紫调']]) {
      expect(html).toMatch(new RegExp(`<input[^>]*type="range"[^>]*aria-label="横向滚动${label}"[^>]*aria-controls="home-hue-${key}"`))
    }
  })

  test('每一排的末尾提供下一组，最后一组回到第一组', () => {
    const html = render([RED, BLUE, PURPLE], 'blue')
    const blue = panelOf(html, 'blue')
    expect(blue).toContain('蓝调就这些')
    expect(blue).toContain('下一组 · 紫调 3 部')
    expect(blue).toContain('看紫调')
    const purple = panelOf(html, 'purple')
    expect(purple).toContain('紫调就这些')
    expect(purple).toContain('下一组 · 红调 8 部')
  })

  test('只有一组时没有地方可去：没有末尾面板', () => {
    const html = render([BLUE])
    expect(cardsIn(panelOf(html, 'blue'))).toHaveLength(15)
    expect(html).not.toContain('就这些')
  })

  test('只有选中的那一组对读屏可见；其余的 inert', () => {
    const html = render([RED, BLUE, PURPLE], 'blue')
    expect(html.match(/aria-pressed="true"/g)).toHaveLength(1)
    const exposed = html.match(/<div[^>]*data-active="true"[^>]*>/g) ?? []
    expect(exposed.filter((tag) => !/aria-hidden="true"/.test(tag) && !/inert/.test(tag))).toHaveLength(1)
  })

  test('只有正在显示的那一组加载封面，其余的等打开', () => {
    const html = render([RED, BLUE, PURPLE], 'blue')
    const imgs = (panel: string) => panel.match(/<img\b/g) ?? []
    expect(imgs(panelOf(html, 'blue'))).toHaveLength(15)
    expect(imgs(panelOf(html, 'red'))).toHaveLength(0)
    expect(imgs(panelOf(html, 'purple'))).toHaveLength(0)
    expect(cardsIn(panelOf(html, 'red'))).toHaveLength(8)
  })

  test('没有一部有颜色时什么都不画', () => {
    expect(render([], 'blue')).toBe('')
  })
})
