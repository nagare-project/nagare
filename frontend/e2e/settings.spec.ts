import { expect, test } from '@playwright/test'
import { VIEWPORT_HEIGHT, WIDTHS, horizontalOverflow, openApp, watchProblems } from './helpers'
import { blockingViolations } from './axe'
import { patchData, stubJson } from './stubs'

/**
 * 设置页（DESIGN.md「「我的」与设置」）：账号 + 三组（媒体库与 NAS / 播放 / 关于）+ 桌面多出的四项。
 * 每个分区都整页过一遍无障碍门槛；分区的切换、焦点去向、mpv 检测、退出确认都按用户的操作走一遍。
 */

/** 设置分类导航里的分区（「扩展」是另一页，单独测） */
const SECTIONS = [
  { name: '账号', hash: 'account' },
  { name: '媒体文件夹', hash: 'folders' },
  { name: '播放器', hash: 'player' },
  { name: '画质增强', hash: 'playback' },
  { name: '磁力播放', hash: 'torrent' },
  { name: '来源', hash: 'sources' },
  { name: '更新', hash: 'update' },
  { name: '后台与退出', hash: 'background' },
  { name: '关于 nagare', hash: 'about' },
] as const

for (const width of WIDTHS) {
  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height: VIEWPORT_HEIGHT } })

    test('逐个分区点过去：地址带上 #hash、只有一个当前项、不溢出、没有错误', async ({ page }) => {
      const problems = watchProblems(page)
      await openApp(page, '/settings')
      const nav = page.getByRole('navigation', { name: '设置分类' })
      for (const { name, hash } of SECTIONS) {
        await nav.getByRole('link', { name, exact: true }).click()
        await expect(page).toHaveURL((url) => url.pathname === '/settings' && url.hash === `#${hash}`)
        await expect(page.getByRole('heading', { level: 2, name, exact: true })).toBeVisible()
        await expect(nav.locator('a[aria-current="page"]')).toHaveCount(1)
        await expect(nav.locator('a[aria-current="page"]')).toHaveText(name)
        expect(await horizontalOverflow(page), `${name} 出现了横向滚动`).toBeLessThanOrEqual(0)
      }
      expect(problems.errors).toEqual([])
    })

    // 每一区单独整页扫一遍（隐藏的分区不在无障碍树里，只扫默认那一区等于没扫）
    for (const { name, hash } of SECTIONS) {
      test(`#${hash}（${name}）整页无 serious / critical 问题`, async ({ page }) => {
        await openApp(page, `/settings#${hash}`)
        await expect(page.getByRole('heading', { level: 2, name, exact: true })).toBeVisible()
        expect(await blockingViolations(page)).toEqual([])
      })
    }
  })
}

test.describe('宽屏（1440px）', () => {
  test.use({ viewport: { width: 1440, height: VIEWPORT_HEIGHT } })

  test('只用键盘切换分区：回车之后焦点落到新分区的标题，下一个 Tab 进到分区里', async ({ page }) => {
    await openApp(page, '/settings')
    const nav = page.getByRole('navigation', { name: '设置分类' })
    // 从「账号」开始往下 Tab，经过媒体文件夹，到播放器
    await nav.getByRole('link', { name: '账号', exact: true }).focus()
    await page.keyboard.press('Tab')
    await expect(nav.getByRole('link', { name: '媒体文件夹', exact: true })).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(nav.getByRole('link', { name: '播放器', exact: true })).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL((url) => url.hash === '#player')
    await expect(page.getByRole('heading', { level: 2, name: '播放器' })).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: '重新检测' })).toBeFocused()
    // 浏览器后退回到上一个分区，导航的当前项跟着变
    await page.goBack()
    await expect(page.getByRole('heading', { level: 2, name: '账号' })).toBeVisible()
    await expect(nav.locator('a[aria-current="page"]')).toHaveText('账号')
  })

  test('「扩展」从设置进：是另一页，带回设置的链接', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/settings')
    await page.getByRole('navigation', { name: '设置分类' }).getByRole('link', { name: '扩展', exact: true }).click()
    await expect(page).toHaveURL((url) => url.pathname === '/extensions')
    await expect(page.getByRole('heading', { level: 1, name: '扩展' })).toBeVisible()
    await page.getByRole('main').getByRole('link', { name: '设置', exact: true }).click()
    await expect(page).toHaveURL((url) => url.pathname === '/settings' && url.hash === '#sources')
    await expect(page.getByRole('heading', { level: 2, name: '来源' })).toBeVisible()
    expect(problems.errors).toEqual([])
  })

  test('mpv 没找到：安装命令能复制；装好后点「重新检测」，状态变成已找到', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    let installed = false
    const missing = {
      found: false,
      hint: '未找到 mpv（已检查 PATH 与常见安装位置）：请先安装 mpv',
      install: { command: 'brew install mpv', url: 'https://mpv.io/installation/', note: '用 Homebrew 安装最省事。' },
    }
    const found = { found: true, version: '0.41.0', path: '/opt/homebrew/bin/mpv', source: 'path' }
    await patchData(page, '/api/settings', (data) => ({ ...data, mpv: installed ? found : missing }))
    await stubJson(page, 'POST', '/api/mpv/detect', () => {
      installed = true
      return found
    })
    const problems = watchProblems(page)
    await openApp(page, '/settings#player')

    const section = page.getByRole('region', { name: '播放器' })
    await expect(section.getByText('未找到', { exact: true })).toBeVisible()
    await expect(section.getByText('brew install mpv')).toBeVisible()
    expect(await blockingViolations(page), 'mpv 缺失时的播放器分区').toEqual([])

    await section.getByRole('button', { name: '复制', exact: true }).click()
    await expect(section.getByRole('button', { name: '已复制 ✓' })).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('brew install mpv')

    await section.getByRole('button', { name: '重新检测' }).click()
    await expect(section.getByRole('status').filter({ hasText: '已找到 mpv 0.41.0' })).toBeVisible()
    await expect(section.getByText('已找到 ✓')).toBeVisible()
    await expect(section.getByText('brew install mpv')).toHaveCount(0)
    expect(problems.errors).toEqual([])
  })

  test('退出确认：确认区出来时焦点在「取消」，取消后回到「退出 nagare」，不发退出请求', async ({ page }) => {
    let shutdown = 0
    await stubJson(page, 'POST', '/api/shutdown', () => {
      shutdown += 1
      return {}
    })
    await openApp(page, '/settings#background')
    const section = page.getByRole('region', { name: '后台与退出' })
    await expect(section.getByText('Dock 图标与菜单栏图标')).toBeVisible()

    await section.getByRole('button', { name: '退出 nagare' }).click()
    await expect(section.getByRole('group', { name: '确认退出' })).toBeVisible()
    await expect(section.getByRole('button', { name: '取消' })).toBeFocused()
    expect(await blockingViolations(page), '退出确认展开时').toEqual([])

    await page.keyboard.press('Enter')
    await expect(section.getByRole('group', { name: '确认退出' })).toHaveCount(0)
    await expect(section.getByRole('button', { name: '退出 nagare' })).toBeFocused()
    expect(shutdown).toBe(0)
  })

  test('账号：没登录时登录表单的标签、说明都在；切走再回来，草稿还在', async ({ page }) => {
    await openApp(page, '/settings#account')
    const email = page.getByLabel('邮箱')
    await email.fill('test@example.com')
    await expect(page.getByRole('note').filter({ hasText: '不能同时保持登录' })).toBeVisible()
    await page.getByRole('navigation', { name: '设置分类' }).getByRole('link', { name: '关于 nagare' }).click()
    await expect(page.getByRole('heading', { level: 2, name: '关于 nagare' })).toBeVisible()
    await page.goBack()
    await expect(page.getByLabel('邮箱')).toHaveValue('test@example.com')
  })
})
