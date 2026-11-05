import { expect, test } from '@playwright/test'
import type { Locator, Page } from '@playwright/test'
import { blockingViolations } from './axe'
import { VIEWPORT_HEIGHT, WIDTHS, horizontalOverflow, openApp, watchProblems } from './helpers'

/**
 * 目录作品页（/entry?id=）与它的磁力抽屉（DESIGN.md「作品页」「作品页 · 磁力」）。
 * 假数据里 1001 = 星屑的旅人：本地有第 1–3 集（第 2 集看到一半），磁力有两个字幕组。
 */

const ENTRY = '/entry?id=1001'

const drawerOf = (page: Page) => page.getByRole('dialog', { name: '磁力边下边播 星屑的旅人' })
const magnetButton = (page: Page) => page.getByRole('button', { name: /^磁力/ })

/** 焦点落在了抽屉背后的页面上（抽屉本身挂在 main 里，抽屉里的不算） */
async function focusBehind(page: Page, drawer: Locator): Promise<boolean> {
  const inDrawer = await drawer.evaluate((el) => el.contains(document.activeElement))
  const inMain = await page.getByRole('main').evaluate((el) => el.contains(document.activeElement))
  return inMain && !inDrawer
}

for (const width of WIDTHS) {
  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height: VIEWPORT_HEIGHT } })

    test('磁力抽屉：键盘打开 → 按字幕组列出 → 焦点出不去 → Esc 关掉，焦点回到「磁力」', async ({ page }) => {
      const problems = watchProblems(page)
      await openApp(page, ENTRY)
      const trigger = magnetButton(page)
      await trigger.focus()
      await page.keyboard.press('Enter')
      const drawer = drawerOf(page)
      await expect(drawer).toBeVisible()
      await expect(drawer.getByRole('group', { name: '选择字幕组' }).getByRole('button')).toHaveCount(2)
      // 宽屏是右侧抽屉，手机（<600px）是底部面板（等滑入动画走完再量）
      await drawer.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
      const box = (await drawer.boundingBox())!
      // 贴着窗口的右边 / 底边（页面锁住滚动时滚动条那一条也让出来）
      const viewport = await page.evaluate(() => ({ width: window.innerWidth, height: window.innerHeight }))
      if (width < 600) {
        expect(Math.round(box.width)).toBe(viewport.width)
        expect(Math.round(box.y + box.height)).toBe(viewport.height)
      } else {
        expect(Math.round(box.x + box.width)).toBe(viewport.width)
        expect(box.width).toBeLessThanOrEqual(640)
      }
      // 模态：一路 Tab 下去，焦点只在抽屉里（或浏览器自己的界面上），不会落到背后的页面
      expect(await drawer.evaluate((el) => el.contains(document.activeElement))).toBe(true)
      for (let i = 0; i < 14; i++) {
        await page.keyboard.press('Tab')
        expect(await focusBehind(page, drawer), `第 ${i + 1} 次 Tab 落到了抽屉背后`).toBe(false)
      }
      expect(await page.evaluate(() => getComputedStyle(document.documentElement).overflow), '抽屉开着时页面不跟着滚').toBe('hidden')
      await page.keyboard.press('Escape')
      await expect(drawer).toBeHidden()
      await expect(trigger).toBeFocused()
      expect(await horizontalOverflow(page), '页面出现了横向滚动').toBeLessThanOrEqual(0)
      expect(problems.errors).toEqual([])
    })
  })
}

for (const width of [1440, 390] as const) {
  test.describe(`${width}px：无障碍`, () => {
    test.use({ viewport: { width, height: VIEWPORT_HEIGHT } })

    test('磁力抽屉开着时整页无 serious / critical 问题', async ({ page }) => {
      await openApp(page, ENTRY)
      await magnetButton(page).click()
      const drawer = drawerOf(page)
      await expect(drawer.getByRole('group', { name: /的资源$/ })).toBeVisible()
      // 等滑入动画走完再扫（动画中途的半透明会让对比度算错）
      await drawer.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
      expect(await blockingViolations(page)).toEqual([])
    })
  })
}

test.describe('宽屏（1440px）', () => {
  test.use({ viewport: { width: 1440, height: VIEWPORT_HEIGHT } })

  test('hero 主按钮播本地的下一集：发出 POST /api/play；海报色写在页面根上', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, ENTRY)
    expect(await page.getByRole('main').evaluate((el) => el.style.getPropertyValue('--poster-tone'))).toBe('#a2b6e3')
    // 集数列表叠着本地状态：看完 / 看到一半 / 库里没有
    const episodes = page.getByRole('region', { name: '剧集' })
    await expect(episodes.getByRole('button', { name: /^播放第 01 集：/ })).toContainText('已看完')
    await expect(episodes.getByRole('button', { name: /^播放第 02 集：/ })).toContainText('看到 43%')
    const played = page.waitForRequest((req) => req.method() === 'POST' && new URL(req.url()).pathname === '/api/play')
    await page.getByRole('button', { name: '继续看第 2 集' }).click()
    expect((await played).postDataJSON()).toMatchObject({ fileId: 'file-2' })
    expect(problems.errors).toEqual([])
  })

  test('抽屉里点「播放」：发出 POST /api/torrent/play（带这一条的磁力与作品身份），抽屉关掉', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, ENTRY)
    await magnetButton(page).click()
    const drawer = drawerOf(page)
    // 默认选中版本最多的字幕组（第一次有结果时就定下来）
    const testSub = drawer.getByRole('group', { name: 'TestSub 的资源' })
    await expect(testSub.getByRole('listitem')).toHaveCount(2)
    const started = page.waitForRequest((req) => req.method() === 'POST' && new URL(req.url()).pathname === '/api/torrent/play')
    await testSub.getByRole('listitem').filter({ hasText: '- 03' }).getByRole('button', { name: '播放' }).click()
    const body = (await started).postDataJSON() as Record<string, unknown>
    expect(body).toMatchObject({ magnet: expect.stringContaining('2222222222222222222222222222222222222222'), anilistId: 1001 })
    await expect(drawer).toBeHidden()
    expect(problems.errors).toEqual([])
  })

  test('抽屉：换字幕组；点遮罩关闭，在抽屉里按下拖到遮罩上松手不关', async ({ page }) => {
    await openApp(page, ENTRY)
    const trigger = magnetButton(page)
    await trigger.click()
    const drawer = drawerOf(page)
    await drawer.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
    await drawer.getByRole('button', { name: /^字幕组 OtherSub/ }).click()
    await expect(drawer.getByRole('group', { name: 'OtherSub 的资源' }).getByRole('listitem')).toHaveCount(1)

    const box = (await drawer.boundingBox())!
    await page.mouse.move(box.x + 120, 160)
    await page.mouse.down()
    await page.mouse.move(80, 160)
    await page.mouse.up()
    await expect(drawer).toBeVisible()

    await page.mouse.click(80, 400)
    await expect(drawer).toBeHidden()
    await expect(trigger).toBeFocused()
  })

  test('没登录账号：作品页说一次「登录后显示弹幕、同步进度」，关掉后刷新也不再出现；整页无 serious / critical 问题', async ({ page }) => {
    await page.route('**/api/lists', (route) => route.fulfill({ json: { success: true, data: { loggedIn: false, entries: [] } } }))
    await openApp(page, ENTRY)
    const hint = page.getByText(/^登录后显示弹幕、同步进度/)
    await expect(hint).toBeVisible()
    expect(await blockingViolations(page)).toEqual([])
    await page.getByRole('button', { name: '关闭这条说明' }).click()
    await expect(hint).toBeHidden()
    await page.reload()
    await page.waitForLoadState('networkidle')
    await expect(page.getByRole('heading', { level: 1, name: '星屑的旅人' })).toBeVisible()
    await expect(page.getByText(/^登录后显示弹幕、同步进度/)).toHaveCount(0)
  })

  test('作品详情读不到：说出原因，给媒体库里这部作品的本地分组链接；整页无 serious / critical 问题', async ({ page }) => {
    await page.route('**/api/anime/1001', (route) => route.fulfill({ status: 503, json: { success: false, data: null, error: '目录服务暂时连不上' } }))
    await openApp(page, ENTRY)
    await expect(page.getByRole('alert')).toContainText('目录服务暂时连不上')
    const local = page.getByRole('link', { name: '星屑的旅人（3 集）' })
    await expect(local).toHaveAttribute('href', '/anime/stardust')
    expect(await blockingViolations(page)).toEqual([])
    await local.click()
    await expect(page).toHaveURL((url) => url.pathname === '/anime/stardust')
  })

  test('在线播放标签照常可用：切过去面板出现，没有错误', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, ENTRY)
    await page.getByRole('tab', { name: '在线播放' }).click()
    await expect(page.getByRole('tabpanel', { name: '在线播放' })).toBeVisible()
    await expect(page.getByRole('tabpanel', { name: '本地媒体库' })).toBeHidden()
    await page.waitForLoadState('networkidle')
    expect(problems.errors).toEqual([])
  })
})
