import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, test } from 'vitest'

// 「今天的番突然不见了」。
//
// 手机上这一排曾经把已播的一半折进一张卡：
//   .rail[data-aired="collapsed"] .card[data-state="aired"] { display: none; }
// 而这一排每分钟按时钟重排一次卡片状态。于是页面开着时刚播出的那张，在它播出的那一分钟变成 aired、
// 从这一排掉出去 —— 正好是读者在等它的时候。
//
// 折叠已经去掉了。这是对这一排的卡穿的几个样式表的检查：任何宽度（任何 @media）都不能再藏起一张卡。
// 选择器对准卡片、或按卡片状态选的规则，不许把它从页面上拿掉。渲染测试看不到这一点 —— 标记一直是完整的，卡是被 CSS 藏起来的。
//
// 按色调逛同一条规矩：它的样式表曾经在宽屏只留一组的前六张、窄屏更少，而胶囊上写着整组有几部，又没有办法看到其余的。

const FILES = ['TodayRail.module.css', 'HueBrowser.module.css', 'cards.module.css']

/** 把元素从页面上拿掉（而不是改样式）的声明 */
const HIDES = /(?:^|[;{\s])(?:display\s*:\s*none|visibility\s*:\s*hidden|content-visibility\s*:\s*hidden)/

/** 对准一张卡的选择器：它的类名，或者时钟会翻的那个状态 */
const TARGETS_CARD = /\.card\b|data-state/

interface Rule {
  file: string
  selector: string
  body: string
}

function rulesOf(file: string, css: string): Rule[] {
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, '')
  return [...bare.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, selector, body]) => ({
    file,
    selector: (selector ?? '').trim().replace(/\s+/g, ' '),
    body: body ?? '',
  }))
}

function hidesACard(rule: Rule): boolean {
  return TARGETS_CARD.test(rule.selector) && HIDES.test(rule.body)
}

const DIR = join(process.cwd(), 'src/components/home')
const rules = FILES.flatMap((file) => rulesOf(file, readFileSync(join(DIR, file), 'utf8')))

describe('发现页横排的样式表', () => {
  test('扫得到卡片规则（守的是解析器，不是 CSS）', () => {
    expect(rules.filter((r) => /\.card\b/.test(r.selector)).length).toBeGreaterThanOrEqual(3)
  })

  test('认得出曾经藏起已播卡片的那条规则', () => {
    const old = rulesOf('old', `@media (max-width: 600px) { .rail[data-aired="collapsed"] .card[data-state="aired"] { display: none; } }`)
    expect(old.some(hidesACard)).toBe(true)
    // ……普通的卡片规则放过
    expect(rulesOf('ok', '.card { flex: none; width: 148px; }').some(hidesACard)).toBe(false)
  })

  test('认得出曾经把按色调逛限制成六张的那条规则', () => {
    expect(rulesOf('old', '.card:nth-child(n + 7) { display: none; }').some(hidesACard)).toBe(true)
  })

  test('任何宽度下都没有规则藏起一张卡', () => {
    expect(rules.filter(hidesACard).map((r) => `${r.file} — ${r.selector}`)).toEqual([])
  })
})
