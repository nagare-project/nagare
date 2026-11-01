import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { ROUTES, VIEWPORT_HEIGHT, openApp } from './helpers'

/**
 * 无障碍门槛（DESIGN.md「无障碍」）：serious + critical 为零，不对任何规则开豁免。
 * 扫 WCAG 2.x A / AA 规则（axe 的 best-practice 一类不算门槛）。
 */
const WCAG_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']
const BLOCKING = new Set(['serious', 'critical'])

/**
 * 整页还过不了门槛的页面：这一批只换了外壳（顶栏 / 抽屉 / token），页面本身按 DESIGN.md 重写是后面几步的事。
 * 这些页面照样整页扫一遍，结果记在报告里（不挡合并）；外壳部分（顶栏、抽屉、展开的搜索）照样是硬门槛。
 *
 * TODO（界面重写的后续几步）：页面重写时把它从这里删掉，整页门槛就自动生效 —— 只许删，不许加。
 * 发现 / 季度 / 放送表 / 我的追番已经重写、移出；剩下的问题（2026-11 首次接入扫描时）：
 * - label-content-name-mismatch：媒体库海报、分组页剧集卡的播放按钮、作品页简介 / 预告片入口 ——
 *   aria-label 没有包含可见文字。
 */
const PAGE_LEVEL_PENDING = new Set<string>([
  '/library',
  '/anime/stardust',
  '/entry?id=1001',
])

async function blockingViolations(page: Page, include?: string): Promise<string[]> {
  const builder = new AxeBuilder({ page }).withTags(WCAG_TAGS)
  const { violations } = await (include === undefined ? builder : builder.include(include)).analyze()
  return violations
    .filter((v) => BLOCKING.has(v.impact ?? ''))
    .map((v) => `${v.impact} ${v.id}：${v.help}（${v.nodes.length} 处：${v.nodes.slice(0, 3).map((n) => n.target.join(' ')).join(' | ')}）`)
}

for (const width of [1440, 390] as const) {
  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height: VIEWPORT_HEIGHT } })

    for (const route of ROUTES) {
      test(`${route}：顶栏无 serious / critical 问题${PAGE_LEVEL_PENDING.has(route) ? '' : '，整页也没有'}`, async ({ page }) => {
        await openApp(page, route)
        expect(await blockingViolations(page, 'header.app-header'), '顶栏').toEqual([])

        const pageLevel = await blockingViolations(page)
        if (!PAGE_LEVEL_PENDING.has(route)) {
          expect(pageLevel, '整页').toEqual([])
          return
        }
        // 待重写的页面：如实记下还剩什么，让报告里看得见；修好了就该把它移出 PAGE_LEVEL_PENDING
        test.info().annotations.push({ type: '整页 axe（待页面重写）', description: pageLevel.join('\n') || '已经没有问题：把这个路由移出 PAGE_LEVEL_PENDING' })
        expect(pageLevel.length, `${route} 整页已经没有问题了：把它移出 PAGE_LEVEL_PENDING，让整页门槛生效`).toBeGreaterThan(0)
      })
    }
  })
}

test.describe('目录页的其他状态（1440px）', () => {
  test.use({ viewport: { width: 1440, height: VIEWPORT_HEIGHT } })

  test('发现页离线 + 媒体库为空：细条与「去添加」整页无 serious / critical 问题', async ({ page }) => {
    await page.route('**/api/library', (route) => route.fulfill({ json: { success: true, data: { folders: [], clusters: [], continueWatching: [], scannedAt: null } } }))
    await page.route('**/api/discover', async (route) => {
      const response = await route.fetch()
      const envelope = (await response.json()) as { data: Record<string, unknown> }
      await route.fulfill({ response, json: { ...envelope, data: { ...envelope.data, freshness: { fetchedAt: new Date(Date.now() - 3600_000).toISOString(), stale: false, offline: true } } } })
    })
    await openApp(page, '/')
    await expect(page.getByRole('link', { name: '去添加' })).toBeVisible()
    expect(await blockingViolations(page)).toEqual([])
  })

  test('作品卡浮层打开时整页无 serious / critical 问题', async ({ page }) => {
    await openApp(page, '/')
    const card = page.getByRole('region', { name: '本季新番' }).getByRole('listitem').first()
    await card.hover()
    await expect(card.getByRole('button', { name: /^播放 / })).toBeVisible()
    expect(await blockingViolations(page)).toEqual([])
  })

  test('放送表的日历设置展开时整页无 serious / critical 问题', async ({ page }) => {
    await openApp(page, '/schedule')
    await page.getByRole('button', { name: '日历设置' }).click()
    await expect(page.getByRole('region', { name: '日历设置' })).toBeVisible()
    expect(await blockingViolations(page)).toEqual([])
  })
})

test.describe('抽屉打开时（390px）', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('抽屉无 serious / critical 问题', async ({ page }) => {
    await openApp(page, '/library')
    await page.getByRole('button', { name: '打开导航' }).click()
    await expect(page.getByRole('dialog', { name: '导航菜单' })).toBeVisible()
    expect(await blockingViolations(page, 'dialog.nav-drawer')).toEqual([])
  })
})

test.describe('顶栏搜索展开时（1440px）', () => {
  test.use({ viewport: { width: 1440, height: VIEWPORT_HEIGHT } })

  test('展开的搜索框无 serious / critical 问题', async ({ page }) => {
    await openApp(page, '/library')
    await page.getByRole('button', { name: '搜索作品' }).click()
    await expect(page.getByRole('banner').getByRole('textbox', { name: '作品名' })).toBeFocused()
    expect(await blockingViolations(page, 'header.app-header')).toEqual([])
  })
})
