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

test.describe('手机（390px）', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('从别处直达 #about：横向一行的设置分类把当前那一项滚进视野', async ({ page }) => {
    await openApp(page, '/settings#about')
    const nav = page.getByRole('navigation', { name: '设置分类' })
    const current = nav.locator('a[aria-current="page"]')
    await expect(current).toHaveText('关于 nagare')
    // 导航自己是横向滚动的，当前项在它的可见范围里
    const [box, view] = await Promise.all([current.boundingBox(), nav.boundingBox()])
    expect(box && view).toBeTruthy()
    expect(box!.x).toBeGreaterThanOrEqual(view!.x - 1)
    expect(box!.x + box!.width).toBeLessThanOrEqual(view!.x + view!.width + 1)
    expect(await nav.evaluate((el) => el.scrollLeft)).toBeGreaterThan(0)
    expect(await horizontalOverflow(page)).toBeLessThanOrEqual(0)
  })
})

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

  test('点当前所在的分区：焦点移到这一区的标题上，地址不变', async ({ page }) => {
    await openApp(page, '/settings#torrent')
    const nav = page.getByRole('navigation', { name: '设置分类' })
    await nav.getByRole('link', { name: '磁力播放', exact: true }).click()
    await expect(page.getByRole('heading', { level: 2, name: '磁力播放' })).toBeFocused()
    await expect(page).toHaveURL((url) => url.hash === '#torrent')
  })

  test('两处内联确认都能按 Esc 取消：确认区收起，焦点回到打开它的按钮，不发请求', async ({ page }) => {
    let requests = 0
    await stubJson(page, 'POST', '/api/shutdown', () => { requests += 1; return {} })
    await stubJson(page, 'POST', '/api/cover-cache/clear', () => { requests += 1; return { bytes: 0, count: 0, maxBytes: 0, skipped: 0 } })
    await openApp(page, '/settings#background')
    const quit = page.getByRole('region', { name: '后台与退出' })
    await quit.getByRole('button', { name: '退出 nagare' }).click()
    await expect(quit.getByRole('button', { name: '取消' })).toBeFocused()
    // Esc 落在确认区的哪个按钮上都一样
    await page.keyboard.press('Shift+Tab')
    await expect(quit.getByRole('button', { name: '确认退出' })).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(quit.getByRole('group', { name: '确认退出' })).toHaveCount(0)
    await expect(quit.getByRole('button', { name: '退出 nagare' })).toBeFocused()

    await page.getByRole('navigation', { name: '设置分类' }).getByRole('link', { name: '关于 nagare', exact: true }).click()
    const about = page.getByRole('region', { name: '关于 nagare' })
    await about.getByRole('button', { name: '清空封面缓存' }).click()
    await expect(about.getByRole('button', { name: '取消' })).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(about.getByRole('group', { name: '确认清空封面缓存' })).toHaveCount(0)
    await expect(about.getByRole('button', { name: '清空封面缓存' })).toBeFocused()
    expect(requests).toBe(0)
  })

  test('请求在途的按钮不会把焦点丢到 <body>：删文件夹失败、清空封面缓存进行中，焦点都留在按下的那颗上', async ({ page }) => {
    let release: () => void = () => {}
    const held = new Promise<void>((resolve) => { release = resolve })
    await page.route((url) => url.pathname.startsWith('/api/library/folders/'), async (route) => {
      if (route.request().method() !== 'DELETE') return route.fallback()
      await new Promise((resolve) => setTimeout(resolve, 400))
      await route.fulfill({ status: 409, json: { success: false, data: null, error: '文件夹正在扫描，稍后再删' } })
    })
    await page.route((url) => url.pathname === '/api/cover-cache/clear', async (route) => {
      await held
      await route.fulfill({ json: { success: true, data: { bytes: 0, count: 0, maxBytes: 314_572_800, skipped: 0 } } })
    })
    await openApp(page, '/settings#folders')
    const remove = page.getByRole('button', { name: /^删除\s*文件夹 / }).first()
    await remove.click()
    const pending = page.getByRole('button', { name: /^删除中/ })
    await expect(pending).toHaveAttribute('aria-disabled', 'true')
    await expect(pending).toBeFocused()
    await expect(page.getByText('文件夹正在扫描，稍后再删')).toBeVisible()
    await expect(remove).toBeFocused()
    await expect(remove).not.toHaveAttribute('aria-disabled', 'true')

    await page.getByRole('navigation', { name: '设置分类' }).getByRole('link', { name: '关于 nagare', exact: true }).click()
    const about = page.getByRole('region', { name: '关于 nagare' })
    await about.getByRole('button', { name: '清空封面缓存' }).click()
    await about.getByRole('button', { name: '确认清空' }).click()
    const clearing = about.getByRole('button', { name: '正在清空 …' })
    await expect(clearing).toHaveAttribute('aria-disabled', 'true')
    await expect(clearing).toBeFocused()
    expect(await page.evaluate(() => document.activeElement === document.body)).toBe(false)
    release()
    await expect(about.getByRole('status').filter({ hasText: '已清空' })).toBeVisible()
    await expect(about.getByRole('button', { name: '清空封面缓存' })).toBeFocused()
  })

  test('登录失败：在密码框里按回车提交，请求期间与失败之后焦点都还在密码框里，改一下再回车就能重试', async ({ page }) => {
    let attempts = 0
    await page.route((url) => url.pathname === '/api/animego/login', async (route) => {
      attempts += 1
      await new Promise((resolve) => setTimeout(resolve, 400))
      await route.fulfill({ status: 401, json: { success: false, data: null, error: '邮箱或密码不对' } })
    })
    await openApp(page, '/settings#account')
    await page.getByLabel('邮箱').fill('you@example.com')
    const password = page.getByLabel('密码')
    await password.fill('wrong')
    await password.press('Enter')
    await expect(page.getByRole('button', { name: '登录中 …' })).toHaveAttribute('aria-disabled', 'true')
    await expect(password).toBeFocused()
    await expect(password).toHaveJSProperty('readOnly', true)
    // 请求中再按回车不会再发一次
    await password.press('Enter')
    await expect(page.getByRole('status').filter({ hasText: '邮箱或密码不对' })).toBeVisible()
    await expect(password).toBeFocused()
    expect(attempts).toBe(1)
    await password.fill('wrong-again')
    await password.press('Enter')
    await expect.poll(() => attempts).toBe(2)
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
