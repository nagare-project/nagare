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
      await expect(page.getByRole('heading', { level: 2, name: '大家在看' })).toBeVisible()
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
    const row = page.getByRole('region', { name: '大家在看' })
    await row.evaluate((el) => { el.setAttribute('data-smoke-mark', '1') })
    await expect(page.getByText('正在刷新')).toBeHidden({ timeout: 15_000 })
    expect(calls).toBe(2)
    await expect(row).toHaveAttribute('data-smoke-mark', '1')
    await expect(page.getByRole('status', { name: '正在读取发现页' })).toHaveCount(0)
  })

  test('横幅：暂停按钮常驻、名字随状态变；圆点点按区域不小于 24px；预览对话框关上后焦点回到按钮', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/')
    const hero = page.getByRole('region', { name: '精选作品' })
    const pause = hero.getByRole('button', { name: '暂停自动切换' })
    await expect(pause).toBeVisible()
    await pause.click()
    await expect(hero.getByRole('button', { name: '继续自动切换' })).toBeVisible()

    const dots = hero.getByRole('button', { name: /^第 \d+ 部：/ })
    expect(await dots.count()).toBeGreaterThan(1)
    for (const box of await dots.evaluateAll((els) => els.map((el) => el.getBoundingClientRect()))) {
      expect(box.width).toBeGreaterThanOrEqual(24)
      expect(box.height).toBeGreaterThanOrEqual(24)
    }
    await dots.nth(1).click()
    await expect(dots.nth(1)).toHaveAttribute('aria-current', 'true')

    const preview = hero.getByRole('button', { name: /^预览 / })
    await preview.click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toBeHidden()
    await expect(preview).toBeFocused()
    expect(problems.errors).toEqual([])
  })

  test('海报行：箭头翻页；鼠标停在卡片上浮出播放与字幕组按钮', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/')
    const row = page.getByRole('region', { name: '大家在看' })
    const next = row.getByRole('button', { name: '大家在看向后翻页' })
    await expect(next).toBeEnabled()
    await next.click()
    await expect(row.getByRole('button', { name: '大家在看向前翻页' })).toBeEnabled()

    const card = page.getByRole('region', { name: '本季新番' }).getByRole('listitem').first()
    await card.hover()
    await expect(card.getByRole('button', { name: /^播放 / })).toBeVisible()
    await expect(card.getByRole('button', { name: /^按字幕组查找磁力/ })).toBeVisible()
    expect(problems.errors).toEqual([])
  })

  test('发现页有一个 h1（视觉上不显示）', async ({ page }) => {
    await openApp(page, '/')
    await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1)
    await expect(page.getByRole('heading', { level: 1, name: '发现' })).toBeAttached()
  })

  test('海报行用键盘翻到头：箭头变成不可点时焦点交给另一头的箭头，不掉到 <body>', async ({ page }) => {
    await openApp(page, '/')
    const row = page.getByRole('region', { name: '大家在看' })
    const next = row.getByRole('button', { name: '大家在看向后翻页' })
    const prev = row.getByRole('button', { name: '大家在看向前翻页' })
    await next.focus()
    // 一直按回车：翻到最后一页的那一下，焦点在箭头变 disabled 之前交给「向前翻页」
    for (let i = 0; i < 40; i += 1) {
      await page.keyboard.press('Enter')
      if (await prev.evaluate((el) => el === document.activeElement)) break
    }
    await expect(next).toBeDisabled()
    await expect(prev).toBeFocused()
    // 往回翻到头也一样
    for (let i = 0; i < 40; i += 1) {
      await page.keyboard.press('Enter')
      if (await next.evaluate((el) => el === document.activeElement)) break
    }
    await expect(prev).toBeDisabled()
    await expect(next).toBeFocused()
  })

  test('作品卡浮层按 Esc 收起（鼠标不用移开）；移开再停回来才重新弹出', async ({ page }) => {
    await openApp(page, '/')
    const card = page.getByRole('region', { name: '本季新番' }).getByRole('listitem').first()
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

  test('预览对话框开着时后台刷新换了数据：横幅不换、对话框不被卸掉；关上后接着自动切换', async ({ page }) => {
    let calls = 0
    await page.route('**/api/discover', async (route) => {
      calls += 1
      const response = await route.fetch()
      const envelope = (await response.json()) as { success: boolean; data: { sections: Array<{ key: string; items: unknown[] }>; freshness?: unknown } }
      // 第一份过期（触发后台再取一次）；第二份把本季作品倒过来排，换掉横幅当前那张
      const data = calls === 1
        ? { ...envelope.data, freshness: { fetchedAt: hoursAgo(1), stale: true, offline: false } }
        : { ...envelope.data, sections: envelope.data.sections.map((section) => section.key === 'thisSeason' ? { ...section, items: [...section.items].reverse() } : section) }
      await route.fulfill({ response, json: { ...envelope, data } })
    })
    await openApp(page, '/')
    const hero = page.getByRole('region', { name: '精选作品' })
    // 横幅当前那张的标题（预览对话框挂在横幅里，它自己也有标题，取第一个）
    const slideTitle = hero.getByRole('heading', { level: 2 }).first()
    const title = await slideTitle.textContent()
    await hero.getByRole('button', { name: /^预览 / }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(page.getByText('正在刷新')).toBeHidden({ timeout: 15_000 })
    expect(calls).toBe(2)
    await expect(dialog).toBeVisible()
    await expect(slideTitle).toHaveText(title ?? '')
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    // 焦点回到「预览」（键盘焦点在横幅里时停住）；移开焦点与鼠标之后换成新数据并接着切换
    await expect(hero.getByRole('button', { name: /^预览 / })).toBeFocused()
    await page.getByRole('heading', { level: 1, name: '发现' }).evaluate((el) => { el.setAttribute('tabindex', '-1'); (el as HTMLElement).focus() })
    await page.mouse.move(0, 800)
    await expect(hero.getByRole('button', { name: '暂停自动切换' })).toBeVisible()
    const after = await slideTitle.textContent()
    await expect(slideTitle).not.toHaveText(after ?? '', { timeout: 12_000 })
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
