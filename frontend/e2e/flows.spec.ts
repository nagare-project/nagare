import { expect, test } from '@playwright/test'
import { VIEWPORT_HEIGHT, openApp, watchProblems } from './helpers'

const PRIMARY = [
  { label: '发现', path: '/' },
  { label: '媒体库', path: '/library' },
  { label: '放送表', path: '/schedule' },
  { label: '季度', path: '/seasonal' },
  { label: '我的追番', path: '/lists' },
] as const

test.describe('宽屏顶栏（1440px）', () => {
  test.use({ viewport: { width: 1440, height: VIEWPORT_HEIGHT } })

  test('主导航逐个点过去：地址对、当前页标记对', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/settings')
    const nav = page.getByRole('navigation', { name: '主导航' })
    for (const { label, path } of PRIMARY) {
      await nav.getByRole('link', { name: label, exact: true }).click()
      await expect(page).toHaveURL((url) => url.pathname === path)
      await expect(nav.getByRole('link', { name: label, exact: true })).toHaveAttribute('aria-current', 'page')
      await expect(nav.locator('a[aria-current="page"]')).toHaveCount(1)
    }
    // 窄屏才有的 ☰ 在宽屏上不出现
    await expect(page.getByRole('button', { name: '打开导航' })).toBeHidden()
    expect(problems.errors).toEqual([])
  })

  test('顶部透明，滚过 8px 出现玻璃；往下滚收起、往上滚滑出', async ({ page }) => {
    await openApp(page, '/')
    const header = page.getByRole('banner')
    await expect(header).not.toHaveAttribute('data-scrolled')

    await page.mouse.wheel(0, 400)
    await expect(header).toHaveAttribute('data-scrolled', '')
    await expect(header).toHaveAttribute('data-hidden', '')

    await page.mouse.wheel(0, -60)
    await expect(header).not.toHaveAttribute('data-hidden')

    await page.evaluate(() => window.scrollTo(0, 0))
    await expect(header).not.toHaveAttribute('data-scrolled')
  })

  test('键盘焦点进到已收起的顶栏时，顶栏立刻出现', async ({ page }) => {
    await openApp(page, '/')
    const header = page.getByRole('banner')
    await page.mouse.wheel(0, 400)
    await expect(header).toHaveAttribute('data-hidden', '')
    await page.getByRole('link', { name: 'nagare（发现）' }).focus()
    await page.keyboard.press('Shift+Tab')
    await page.keyboard.press('Tab')
    await expect(header).not.toHaveAttribute('data-hidden')
  })

  test('顶栏搜索：展开、提交落到发现页的作品搜索，Esc 收起并还焦点', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/library')
    const toggle = page.getByRole('button', { name: '搜索作品' })
    await toggle.click()
    const input = page.getByRole('banner').getByRole('textbox', { name: '作品名' })
    await expect(input).toBeFocused()
    await input.fill('星屑')
    await input.press('Enter')
    await expect(page).toHaveURL((url) => url.pathname === '/' && url.searchParams.get('q') === '星屑')
    await expect(page.getByRole('main')).toContainText('星屑的旅人')
    await expect(page.getByRole('button', { name: '搜索作品' })).toBeFocused()

    await page.getByRole('button', { name: '搜索作品' }).click()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('banner').getByRole('textbox')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '搜索作品' })).toBeFocused()
    expect(problems.errors).toEqual([])
  })

  test('/discover 是 / 的别名：跳过去，查询参数原样保留', async ({ page }) => {
    await openApp(page, '/discover?genre=Action')
    await expect(page).toHaveURL((url) => url.pathname === '/' && url.searchParams.get('genre') === 'Action')
    await expect(page.getByRole('navigation', { name: '主导航' }).getByRole('link', { name: '发现', exact: true })).toHaveAttribute('aria-current', 'page')
  })

  test('设置：逐个分区切换，每次只有一个当前项；扩展从设置进', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/settings')
    const nav = page.getByRole('navigation', { name: '设置分类' })
    const sections = ['应用', '账号', '本地媒体库', '媒体播放器', '磁力播放', '来源', '更新', '关于']
    for (const name of sections) {
      await nav.getByRole('link', { name, exact: true }).click()
      // 分区标题（有的分区里第一张卡片的标题与它同名，取第一个）
      await expect(page.getByRole('heading', { level: 2, name, exact: true }).first()).toBeVisible()
      await expect(nav.locator('a[aria-current="page"]')).toHaveCount(1)
      await expect(nav.locator('a[aria-current="page"]')).toHaveText(name)
    }
    await nav.getByRole('link', { name: '扩展', exact: true }).click()
    await expect(page).toHaveURL((url) => url.pathname === '/extensions')
    await expect(page.getByRole('heading', { level: 1, name: '扩展' })).toBeVisible()
    expect(problems.errors).toEqual([])
  })

  test('媒体库里点播放：发出 POST /api/play，带上这一集的文件 id', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/library')
    const played = page.waitForRequest((req) => req.method() === 'POST' && new URL(req.url()).pathname === '/api/play')
    await page.getByRole('button', { name: /^继续播放 星屑的旅人/ }).click()
    const request = await played
    expect(request.postDataJSON()).toMatchObject({ fileId: 'file-2' })
    // 变更请求必须带自定义头（CSRF 防线）；值是 token，不打印
    expect(request.headers()['x-nagare-token']).toBe(process.env.NAGARE_SMOKE_TOKEN)
    expect(problems.errors).toEqual([])
  })
})

test.describe('窄屏抽屉（390px）', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('☰ 打开抽屉，点里面的链接跳转并关闭，焦点回到 ☰', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/')
    await expect(page.getByRole('navigation', { name: '主导航' })).toBeHidden()
    const menu = page.getByRole('button', { name: '打开导航' })
    await menu.click()
    const drawer = page.getByRole('dialog', { name: '导航菜单' })
    await expect(drawer).toBeVisible()
    await expect(menu).toHaveAttribute('aria-expanded', 'true')

    await drawer.getByRole('link', { name: '媒体库', exact: true }).click()
    await expect(page).toHaveURL((url) => url.pathname === '/library')
    await expect(drawer).toBeHidden()
    await expect(menu).toBeFocused()
    await expect(menu).toHaveAttribute('aria-expanded', 'false')
    expect(problems.errors).toEqual([])
  })

  test('Esc 与点暗处都能关；焦点在抽屉里，打开时页面不能滚', async ({ page }) => {
    await openApp(page, '/library')
    const menu = page.getByRole('button', { name: '打开导航' })
    const drawer = page.getByRole('dialog', { name: '导航菜单' })

    await menu.click()
    await expect(drawer).toBeVisible()
    expect(await page.evaluate(() => document.querySelector('dialog.nav-drawer')?.contains(document.activeElement))).toBe(true)
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).overflow)).toBe('hidden')
    await page.keyboard.press('Escape')
    await expect(drawer).toBeHidden()
    await expect(menu).toBeFocused()

    await menu.click()
    await expect(drawer).toBeVisible()
    // 抽屉最宽 320px：点它右边的暗处
    await page.mouse.click(370, 400)
    await expect(drawer).toBeHidden()
    await expect(menu).toBeFocused()
  })

  test('窗口拉宽到完整顶栏时，开着的抽屉自己关掉', async ({ page }) => {
    await openApp(page, '/library')
    await page.getByRole('button', { name: '打开导航' }).click()
    const drawer = page.getByRole('dialog', { name: '导航菜单' })
    await expect(drawer).toBeVisible()
    await page.setViewportSize({ width: 1280, height: 844 })
    await expect(drawer).toBeHidden()
    await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible()
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).overflow)).not.toBe('hidden')
  })

  test('在抽屉里按下、拖到暗处松手不关', async ({ page }) => {
    await openApp(page, '/library')
    await page.getByRole('button', { name: '打开导航' }).click()
    const drawer = page.getByRole('dialog', { name: '导航菜单' })
    await expect(drawer).toBeVisible()
    // 等滑入动画走完：动画途中抽屉还没到位，按在 x=100 处可能还落在暗处
    await drawer.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
    await page.mouse.move(100, 600)
    await page.mouse.down()
    await page.mouse.move(370, 600)
    await page.mouse.up()
    await expect(drawer).toBeVisible()
  })
})
