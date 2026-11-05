import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

/**
 * 无障碍门槛（DESIGN.md「无障碍」）：serious + critical 为零，不对任何规则开豁免。
 * 扫 WCAG 2.x A / AA 规则（axe 的 best-practice 一类不算门槛）。
 */
const WCAG_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']
const BLOCKING = new Set(['serious', 'critical'])

/**
 * 等页面上会结束的动画都放完（弹窗淡入、抽屉滑入这类）。axe 按当时的计算样式算对比度：淡入到一半的弹窗
 * 文字是半透明的，会被误判成对比度不够（慢的 CI 机器上选集弹窗就这样挂过）。无限循环的动画（缓冲条的脉动、
 * 骨架闪烁）等不完，跳过。
 */
async function settleAnimations(page: Page): Promise<void> {
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().endTime !== Infinity)
        .map((a) => a.finished.catch(() => undefined)),
    ),
  )
}

/** 页面（或 include 指定的一块）里 serious / critical 的问题，一条一行，写清是哪条规则、在哪几处 */
export async function blockingViolations(page: Page, include?: string): Promise<string[]> {
  await settleAnimations(page)
  const builder = new AxeBuilder({ page }).withTags(WCAG_TAGS)
  const { violations } = await (include === undefined ? builder : builder.include(include)).analyze()
  return violations
    .filter((v) => BLOCKING.has(v.impact ?? ''))
    .map((v) => `${v.impact} ${v.id}：${v.help}（${v.nodes.length} 处：${v.nodes.slice(0, 3).map((n) => n.target.join(' ')).join(' | ')}）`)
}
