import { expect, test } from '@playwright/test'
import type { Page, Route } from '@playwright/test'
import { VIEWPORT_HEIGHT, WIDTHS, horizontalOverflow, openApp, watchProblems } from './helpers'

/**
 * 目录类页面（发现 / 季度 / 放送表 / 我的追番）的冒烟（DESIGN.md「发现」「状态表」「动效」）。
 * 假数据里季度页带 freshness.offline（tools/uismoke 的 "@nowiso-7200" = 两小时前）；
 * 发现页的离线 / 过期 / 空媒体库用 page.route 在真实的假数据响应上改一处。
 */

/** 把某个接口的信封 data 改一下再交给页面 */
async function patchData(page: Page, path: string, patch: (data: Record<string, unknown>) => Record<string, unknown>): Promise<void> {
  await page.route(`**${path}`, async (route: Route) => {
    const response = await route.fetch()
    const envelope = (await response.json()) as { success: boolean; data: Record<string, unknown> }
    await route.fulfill({ response, json: { ...envelope, data: patch(envelope.data) } })
  })
}

const hoursAgo = (hours: number) => new Date(Date.now() - hours * 3600_000).toISOString()

/**
 * 离线细条：看得见的那句（不是 live region，相对时间每分钟走一格不打断读屏），
 * 与旁边常驻的播报区（只在进出离线时换字）。两处字一样时用 role 区分。
 */
function freshness(page: Page, text: string) {
  return {
    shown: page.getByText(text, { exact: true }).and(page.locator(':not([role="status"])')),
    // status 的名字只取作者给的（不取内容），按内容筛
    announced: page.getByRole('status').filter({ hasText: new RegExp(`^${text}$`) }),
  }
}

for (const width of WIDTHS) {
  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height: VIEWPORT_HEIGHT } })

    test('季度页离线时显示「离线 · 更新于 …」，内容照常', async ({ page }) => {
      const problems = watchProblems(page)
      await openApp(page, '/seasonal')
      const note = freshness(page, '离线 · 更新于 2 小时前')
      await expect(note.shown).toBeVisible()
      await expect(note.announced).toHaveCount(1)
      await expect(page.getByRole('heading', { level: 1 })).toContainText('季新番')
      await expect(page.getByRole('main').getByRole('link', { name: /^星屑的旅人/ })).toBeVisible()
      expect(await horizontalOverflow(page)).toBeLessThanOrEqual(0)
      expect(problems.errors).toEqual([])
    })

    test('发现页离线时横幅上显示「离线 · 更新于 …」', async ({ page }) => {
      const problems = watchProblems(page)
      await patchData(page, '/api/discover', (data) => ({ ...data, freshness: { fetchedAt: hoursAgo(3), stale: false, offline: true } }))
      await openApp(page, '/')
      const note = freshness(page, '离线 · 更新于 3 小时前')
      await expect(note.shown).toBeVisible()
      await expect(note.announced).toHaveCount(1)
      await expect(page.getByRole('heading', { level: 2, name: '大家都在追' })).toBeVisible()
      expect(await horizontalOverflow(page)).toBeLessThanOrEqual(0)
      expect(problems.errors).toEqual([])
    })

    test('媒体库是空的：发现页顶部一行「去添加」，点了去媒体库', async ({ page }) => {
      const problems = watchProblems(page)
      await page.route('**/api/library', (route) => route.fulfill({ json: { success: true, data: { folders: [], clusters: [], continueWatching: [], scannedAt: null } } }))
      await openApp(page, '/')
      await expect(page.getByText('nagare 播放你自己的番剧文件')).toBeVisible()
      await page.getByRole('link', { name: '去添加' }).click()
      await expect(page).toHaveURL((url) => url.pathname === '/library')
      expect(problems.errors).toEqual([])
    })
  })
}

test.describe('宽屏（1440px）', () => {
  test.use({ viewport: { width: 1440, height: VIEWPORT_HEIGHT } })

  test('过期的一份：先说「正在刷新」，后台再取一次后原地换上新数据（不回到骨架）', async ({ page }) => {
    let calls = 0
    await page.route('**/api/discover', async (route) => {
      calls += 1
      const response = await route.fetch()
      const envelope = (await response.json()) as { success: boolean; data: Record<string, unknown> }
      const data = calls === 1 ? { ...envelope.data, freshness: { fetchedAt: hoursAgo(1), stale: true, offline: false } } : envelope.data
      await route.fulfill({ response, json: { ...envelope, data } })
    })
    await openApp(page, '/')
    await expect(page.getByText('更新于 1 小时前 · 正在刷新', { exact: true })).toBeVisible()
    // 在线时「正在刷新」来来去去不念：播报区一直是空的
    await expect(page.getByRole('status').filter({ hasText: '正在刷新' })).toHaveCount(0)
    // 给第一行做个记号：换数据时整页没有卸载重建，记号还在
    const row = page.getByRole('region', { name: '大家都在追' })
    await row.evaluate((el) => { el.setAttribute('data-smoke-mark', '1') })
    await expect(page.getByText('正在刷新')).toBeHidden({ timeout: 15_000 })
    expect(calls).toBe(2)
    await expect(row).toHaveAttribute('data-smoke-mark', '1')
    await expect(page.getByRole('status', { name: '正在读取发现页' })).toHaveCount(0)
  })

  test('横幅：暂停按钮名字随状态变；短条点按区域不小于 24px，点了立刻换、整页换颜色；「查看详情」进作品页', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/')
    const hero = page.getByRole('region', { name: '本季焦点' })
    const pause = hero.getByRole('button', { name: '暂停自动切换' })
    await expect(pause).toBeVisible()
    await pause.click()
    await expect(hero.getByRole('button', { name: '继续自动切换' })).toBeVisible()

    const bars = hero.getByRole('button', { name: /^本季焦点 \d+：/ })
    await expect(bars).toHaveCount(5)
    for (const box of await bars.evaluateAll((els) => els.map((el) => el.getBoundingClientRect()))) {
      expect(box.width).toBeGreaterThanOrEqual(24)
      expect(box.height).toBeGreaterThanOrEqual(24)
    }
    const ground = () => page.locator('main').evaluate((el) => (el as HTMLElement).style.getPropertyValue('--home-ground'))
    const before = await ground()
    await bars.nth(1).click()
    await expect(bars.nth(1)).toHaveAttribute('aria-pressed', 'true')
    expect(await ground()).not.toBe(before)
    const title = await hero.locator('article[aria-hidden="false"] h2').first().textContent()
    await hero.getByRole('link', { name: /^查看详情：/ }).click()
    await expect(page).toHaveURL((url) => url.pathname === '/entry' && url.searchParams.has('id'))
    expect(title).toBeTruthy()
    expect(problems.errors).toEqual([])
  })

  test('今日更新与按色调逛的横排：键盘在滑块上走、一打开停在「现在」；换一组色调', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/')
    const today = page.locator('#home-today-rail')
    await expect(today.locator('[data-now]')).toBeAttached()
    const scrub = page.getByRole('slider', { name: '横向滚动今日更新' })
    await scrub.focus()
    await page.keyboard.press('End')
    await expect.poll(() => today.evaluate((el) => Math.round(el.scrollWidth - el.clientWidth - el.scrollLeft))).toBeLessThanOrEqual(1)
    await page.keyboard.press('Home')
    await expect.poll(() => today.evaluate((el) => el.scrollLeft)).toBe(0)

    const pills = page.getByRole('group', { name: '按色调筛选' }).getByRole('button')
    expect(await pills.count()).toBeGreaterThan(1)
    const second = pills.nth(1)
    await second.click()
    await expect(second).toHaveAttribute('aria-pressed', 'true')
    const label = (await second.textContent())?.replace(/\d+ 部$/, '').trim() ?? ''
    await expect(page.getByRole('slider', { name: `横向滚动${label}` })).toBeAttached()
    expect(problems.errors).toEqual([])
  })

  test('完结佳作「换一批」：换上另一批，请求中按钮不变 disabled（焦点不丢）', async ({ page }) => {
    await openApp(page, '/')
    const gems = page.getByRole('region', { name: '完结佳作' })
    const first = await gems.locator('[data-active="true"] a').first().textContent()
    const refresh = gems.getByRole('button', { name: '换一批' })
    await refresh.click()
    await expect(refresh).toBeFocused()
    await expect.poll(() => gems.locator('[data-active="true"] a').first().textContent()).not.toBe(first)
  })

  test('发现页有一个 h1（视觉上不显示）', async ({ page }) => {
    await openApp(page, '/')
    await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1)
    await expect(page.getByRole('heading', { level: 1, name: '发现' })).toBeAttached()
  })

  test('作品卡浮层按 Esc 收起（鼠标不用移开）；移开再停回来才重新弹出', async ({ page }) => {
    await openApp(page, '/seasonal')
    const card = page.getByRole('main').getByRole('listitem').first()
    await card.hover()
    const play = card.getByRole('button', { name: /^播放 / })
    await expect(play).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(play).toBeHidden()
    await page.waitForTimeout(600)
    await expect(play).toBeHidden()
    await page.mouse.move(0, 0)
    await card.hover()
    await expect(play).toBeVisible()
  })

  test('后台刷新把本季前五重新排了序：横幅停在原来那一部上，不跳成另一部', async ({ page }) => {
    let calls = 0
    await page.route('**/api/discover', async (route) => {
      calls += 1
      const response = await route.fetch()
      const envelope = (await response.json()) as { success: boolean; data: { seasonal: { items: unknown[] }; freshness?: unknown } }
      // 第一份过期（触发后台再取一次）；第二份把本季前五倒过来排
      const items = envelope.data.seasonal.items
      const data = calls === 1
        ? { ...envelope.data, freshness: { fetchedAt: hoursAgo(1), stale: true, offline: false } }
        : { ...envelope.data, seasonal: { ...envelope.data.seasonal, items: [...items.slice(0, 5).reverse(), ...items.slice(5)] } }
      await route.fulfill({ response, json: { ...envelope, data } })
    })
    await openApp(page, '/')
    const hero = page.getByRole('region', { name: '本季焦点' })
    await hero.getByRole('button', { name: '暂停自动切换' }).click()
    const title = await hero.locator('article[aria-hidden="false"] h2').first().textContent()
    await expect(page.getByText('正在刷新')).toBeHidden({ timeout: 15_000 })
    expect(calls).toBe(2)
    await expect(hero.locator('article[aria-hidden="false"] h2').first()).toHaveText(title ?? '')
  })

  test('我的追番：四个分段带数量，切换后显示对应的作品', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/lists')
    const tabs = page.getByRole('tablist', { name: '观看状态' })
    await expect(tabs.getByRole('tab')).toHaveCount(4)
    await tabs.getByRole('tab', { name: /^看完/ }).click()
    await expect(page.getByRole('main').getByRole('link', { name: /^夜行列车/ })).toBeVisible()
    await tabs.getByRole('tab', { name: /^弃番/ }).click()
    await expect(page.getByText('「弃番」里还没有作品')).toBeVisible()
    expect(problems.errors).toEqual([])
  })

  test('放送表：月历里的条目以可见文字命名，点开是作品预览', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/schedule')
    const entry = page.getByRole('button', { name: /^星屑的旅人\s*第 \d+ 集/ }).first()
    await expect(entry).toBeVisible()
    await entry.click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(entry).toBeFocused()
    expect(problems.errors).toEqual([])
  })
})

test.describe('手机（390px）', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('放送表换成逐日列表；季度页类型标签横向一条不撑出页面', async ({ page }) => {
    await openApp(page, '/schedule')
    await expect(page.getByRole('region', { name: /年.*月.*日/ }).first()).toBeVisible()
    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(0)
    await openApp(page, '/seasonal')
    await expect(page.getByRole('group', { name: '类型' })).toBeVisible()
    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(0)
  })
})
