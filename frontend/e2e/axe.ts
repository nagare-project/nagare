import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

/**
 * 无障碍门槛（DESIGN.md「无障碍」）：serious + critical 为零，不对任何规则开豁免。
 * 扫 WCAG 2.x A / AA 规则（axe 的 best-practice 一类不算门槛）。
 */
const WCAG_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']
const BLOCKING = new Set(['serious', 'critical'])

/** 页面（或 include 指定的一块）里 serious / critical 的问题，一条一行，写清是哪条规则、在哪几处 */
export async function blockingViolations(page: Page, include?: string): Promise<string[]> {
  const builder = new AxeBuilder({ page }).withTags(WCAG_TAGS)
  const { violations } = await (include === undefined ? builder : builder.include(include)).analyze()
  return violations
    .filter((v) => BLOCKING.has(v.impact ?? ''))
    .map((v) => `${v.impact} ${v.id}：${v.help}（${v.nodes.length} 处：${v.nodes.slice(0, 3).map((n) => n.target.join(' ')).join(' | ')}）`)
}
