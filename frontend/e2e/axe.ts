import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

/**
 * 无障碍门槛（DESIGN.md「无障碍」）：serious + critical 为零，不对任何规则开豁免。
 * 扫 WCAG 2.x A / AA 规则（axe 的 best-practice 一类不算门槛）。
 */
const WCAG_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']
const BLOCKING = new Set(['serious', 'critical'])

/** 等动画最多等这么久；页面上最长的动画不到 1 秒，超过就是有动画卡住了 */
const SETTLE_LIMIT_MS = 5_000

/**
 * 等页面上正在播、会结束的动画都放完（弹窗淡入、抽屉滑入这类）。axe 按当时的计算样式算对比度：淡入到一半的弹窗
 * 文字是半透明的，会被误判成对比度不够（慢的 CI 机器上选集弹窗就这样挂过）。
 *
 * 只等正在播的有限动画：无限循环的（缓冲条脉动、骨架闪烁）与滚动驱动的等不完；暂停的停在哪一帧，用户看到的就是
 * 那一帧，照原样扫。等的途中又冒出来的动画（提示条滑入）也一并等。超过上限就直接失败并列出卡住的动画 —— 不能
 * 悄悄接着扫，否则又回到扫到半透明的老问题。
 */
async function settleAnimations(page: Page): Promise<void> {
  const stuck = await page.evaluate(async (limitMs) => {
    const running = () =>
      document.getAnimations().filter((a) => {
        const end = a.effect?.getComputedTiming().endTime
        // 比上限还长的有限动画不是进场 / 淡入，是倒计时（发现页横幅那根 8 秒的指示条）：照原样扫
        return a.playState === 'running' && typeof end === 'number' && Number.isFinite(end) && end <= limitMs
      })
    const deadline = performance.now() + limitMs
    for (let pending = running(); pending.length > 0; pending = running()) {
      const left = deadline - performance.now()
      if (left <= 0) {
        return pending.map((a) => {
          const target = a.effect instanceof KeyframeEffect ? a.effect.target : null
          const where = target ? `${target.tagName.toLowerCase()}${target.className ? `.${String(target.className).trim().split(/\s+/).join('.')}` : ''}` : '?'
          const what = a instanceof CSSAnimation ? a.animationName : a instanceof CSSTransition ? a.transitionProperty : a.id || 'animation'
          return `${where} ${what}`
        })
      }
      await Promise.race([
        Promise.all(pending.map((a) => a.finished.catch(() => undefined))),
        new Promise((resolve) => setTimeout(resolve, left)),
      ])
    }
    return []
  }, SETTLE_LIMIT_MS)
  if (stuck.length > 0) {
    throw new Error(`等了 ${SETTLE_LIMIT_MS}ms 动画还没放完，axe 不扫半路的样子：${stuck.join('；')}`)
  }
}

/**
 * 先停住会自己切换的横幅（发现页「本季焦点」，DESIGN.md「动效」的暂停按钮）：扫的途中它换一张，
 * 新的一张的字正在淡入，会被误判成对比度不够。读者也能这样停住它，扫的是停住之后的样子。
 */
async function holdCarousels(page: Page): Promise<void> {
  const pause = page.getByRole('button', { name: '暂停自动切换' })
  if ((await pause.count()) > 0 && (await pause.first().isVisible())) await pause.first().click()
}

/** 页面（或 include 指定的一块）里 serious / critical 的问题，一条一行，写清是哪条规则、在哪几处 */
export async function blockingViolations(page: Page, include?: string): Promise<string[]> {
  await holdCarousels(page)
  await settleAnimations(page)
  const builder = new AxeBuilder({ page }).withTags(WCAG_TAGS)
  const { violations } = await (include === undefined ? builder : builder.include(include)).analyze()
  return violations
    .filter((v) => BLOCKING.has(v.impact ?? ''))
    .map((v) => `${v.impact} ${v.id}：${v.help}（${v.nodes.length} 处：${v.nodes.slice(0, 3).map((n) => n.target.join(' ')).join(' | ')}）`)
}
