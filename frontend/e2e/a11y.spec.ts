import { expect, test } from '@playwright/test'
import { blockingViolations } from './axe'
import { ROUTES, VIEWPORT_HEIGHT, openApp } from './helpers'

/**
 * 无障碍门槛（DESIGN.md「无障碍」，规则与标签见 axe.ts）：每一条路由的顶栏与整页都要过，
 * 不对任何页面开豁免 —— 页面按 DESIGN.md 重写完，外壳之外的「只记不挡」名单就清空了。
 */
for (const width of [1440, 390] as const) {
  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height: VIEWPORT_HEIGHT } })

    for (const route of ROUTES) {
      test(`${route}：顶栏与整页都没有 serious / critical 问题`, async ({ page }) => {
        await openApp(page, route)
        expect(await blockingViolations(page, 'header.app-header'), '顶栏').toEqual([])
        expect(await blockingViolations(page), '整页').toEqual([])
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
    await openApp(page, '/seasonal')
    const card = page.getByRole('main').getByRole('listitem').first()
    await card.hover()
    await expect(card.getByRole('button', { name: /^播放 / })).toBeVisible()
    expect(await blockingViolations(page)).toEqual([])
  })

  test('发现页：本季高分的详情浮层（键盘聚焦）与换过一组的色调整页无 serious / critical 问题', async ({ page }) => {
    await openApp(page, '/')
    await page.getByRole('group', { name: '按色调筛选' }).getByRole('button').nth(1).click()
    await page.getByRole('region', { name: '本季高分' }).getByRole('link').first().focus()
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

test.describe('设置「关于」分区（390px）', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('关于与封面缓存（含清空确认）无 serious / critical 问题', async ({ page }) => {
    await openApp(page, '/settings#about')
    await expect(page.getByRole('heading', { name: '封面缓存' })).toBeVisible()
    expect(await blockingViolations(page)).toEqual([])
    await page.getByRole('button', { name: '清空封面缓存' }).click()
    await expect(page.getByRole('button', { name: '取消' })).toBeFocused()
    expect(await blockingViolations(page)).toEqual([])
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
