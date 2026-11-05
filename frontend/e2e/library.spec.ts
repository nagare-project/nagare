import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { blockingViolations } from './axe'
import { VIEWPORT_HEIGHT, WIDTHS, horizontalOverflow, openApp, watchProblems } from './helpers'

/**
 * 媒体库与本地分组页的冒烟（DESIGN.md「媒体库」「作品页」）。假数据：星屑的旅人（手动认定、有海报色、在看）、
 * 夜行列车（自动认出、没有海报色）、未整理的文件（没认出、低置信，在折叠的「还没认出」里）。
 */

/** 作品网格的列数：桌面 6 → 平板（≤900px）3 → 手机（≤600px）2 */
const COLUMNS: Record<(typeof WIDTHS)[number], number> = { 1440: 6, 900: 3, 390: 2 }

const unrecognizedToggle = (page: Page) => page.locator('summary', { hasText: /^还没认出的 1 组/ })

for (const width of WIDTHS) {
  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height: VIEWPORT_HEIGHT } })

    test('媒体库：继续观看 → 作品网格 → 折叠的「还没认出」，网格列数对，不溢出', async ({ page }) => {
      const problems = watchProblems(page)
      await openApp(page, '/library')
      const cont = page.getByRole('region', { name: '继续观看' })
      const grid = page.getByRole('list', { name: '媒体库' })
      const toggle = unrecognizedToggle(page)
      await expect(cont.getByRole('button', { name: /^继续播放 星屑的旅人/ })).toBeVisible()
      await expect(grid.getByRole('link')).toHaveCount(2)
      await expect(toggle).toBeVisible()

      // 自上而下：继续观看 → 网格 → 还没认出
      const tops = await Promise.all([cont, grid, toggle].map(async (part) => (await part.boundingBox())!.y))
      expect([...tops].sort((a, b) => a - b)).toEqual(tops)

      expect(await grid.evaluate((el) => getComputedStyle(el).gridTemplateColumns.split(' ').length)).toBe(COLUMNS[width])
      // 认出的卡片带海报色（写在卡片根元素上），没有色阶的落回中性值
      const toned = grid.getByRole('listitem').filter({ hasText: '星屑的旅人' })
      const plain = grid.getByRole('listitem').filter({ hasText: '夜行列车' })
      expect(await toned.evaluate((el) => getComputedStyle(el).getPropertyValue('--poster-tone-mid').trim())).toBe('#4f6290')
      expect(await plain.evaluate((el) => getComputedStyle(el).getPropertyValue('--poster-tone-mid').trim())).toBe('#696969')

      await expect(toggle.locator('xpath=..')).not.toHaveAttribute('open', '')
      expect(await horizontalOverflow(page), '页面出现了横向滚动').toBeLessThanOrEqual(0)
      expect(problems.errors).toEqual([])
    })

    test('「还没认出」展开后整页无 serious / critical 问题，也不溢出', async ({ page }) => {
      await openApp(page, '/library')
      await unrecognizedToggle(page).click()
      const row = page.getByRole('link', { name: /^未整理的文件/ })
      await expect(row).toBeVisible()
      await expect(row).toContainText('待确认')
      expect(await blockingViolations(page)).toEqual([])
      expect(await horizontalOverflow(page)).toBeLessThanOrEqual(0)
    })
  })
}

test.describe('宽屏（1440px）', () => {
  test.use({ viewport: { width: 1440, height: VIEWPORT_HEIGHT } })

  test('只用键盘：展开「还没认出」→ 进分组页 → 认作品对话框，Esc 关掉后焦点回到「认作品」', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/library')
    const toggle = unrecognizedToggle(page)
    await toggle.focus()
    await page.keyboard.press('Enter')
    const row = page.getByRole('link', { name: /^未整理的文件/ })
    await page.keyboard.press('Tab')
    await expect(row).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL((url) => url.pathname === '/anime/unknown-folder')

    // 没认出：没有海报，标题取自文件夹，唯一的实心按钮是「认作品」
    await expect(page.getByRole('heading', { level: 1, name: '未整理的文件' })).toBeVisible()
    const pick = page.getByRole('button', { name: '认作品' })
    await pick.focus()
    await page.keyboard.press('Enter')
    const dialog = page.getByRole('dialog', { name: '选择对应的作品' })
    await expect(dialog).toBeVisible()
    expect(await dialog.evaluate((el) => el.contains(document.activeElement))).toBe(true)
    expect(await blockingViolations(page), '对话框开着时整页').toEqual([])
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(pick).toBeFocused()
    expect(problems.errors).toEqual([])
  })

  test('分组页：键盘在集数列表里播一集，发出 POST /api/play；海报色写在页面根上，焦点环仍是 #0a84ff', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/anime/stardust')
    const main = page.getByRole('main')
    expect(await main.evaluate((el) => el.style.getPropertyValue('--poster-tone'))).toBe('#a2b6e3')

    // 返回链接之后就是主按钮：用 Tab 走过去（键盘焦点才画焦点环）
    await main.getByRole('link', { name: '媒体库', exact: true }).focus()
    await page.keyboard.press('Tab')
    const primary = page.getByRole('button', { name: '继续看第 2 集' })
    await expect(primary).toBeFocused()
    const ring = await primary.evaluate((el) => { const s = getComputedStyle(el); return `${s.outlineStyle} ${s.outlineWidth} ${s.outlineColor}` })
    expect(ring).toBe('solid 2px rgb(10, 132, 255)')
    // 主按钮的底是这部作品的海报色
    expect(await primary.evaluate((el) => getComputedStyle(el).backgroundColor)).toBe('rgb(162, 182, 227)')

    const played = page.waitForRequest((req) => req.method() === 'POST' && new URL(req.url()).pathname === '/api/play')
    const third = page.getByRole('region', { name: /^本地剧集/ }).getByRole('button', { name: /^播放第 03 集：/ })
    await third.focus()
    await page.keyboard.press('Enter')
    expect((await played).postDataJSON()).toMatchObject({ fileId: 'file-3' })
    expect(problems.errors).toEqual([])
  })

  test('没有海报色的分组页：页面根上的海报色是 DESIGN.md 的中性值', async ({ page }) => {
    await openApp(page, '/anime/night-train')
    const main = page.getByRole('main')
    expect(await main.evaluate((el) => el.getAttribute('style'))).toBeNull()
    expect(await main.evaluate((el) => getComputedStyle(el).getPropertyValue('--poster-tone').trim())).toBe('#b1b1b1')
    expect(await main.evaluate((el) => getComputedStyle(el).getPropertyValue('--poster-on-tone').trim())).toBe('#0b0b0b')
  })
})
