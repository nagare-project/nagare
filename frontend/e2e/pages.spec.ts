import { expect, test } from '@playwright/test'
import { VIEWPORT_HEIGHT, WIDTHS, horizontalOverflow, openApp, watchProblems } from './helpers'
import { blockingViolations } from './axe'
import { stubJson } from './stubs'

/**
 * 搜索、磁力任务、同步横幅、新手引导、未授权等「其余页面」的冒烟：
 * 在固定假数据之上换出用户会遇到的状态（有结果、有会话、同步失败、库是空的……），
 * 每个状态都整页（或那一块）过一遍无障碍门槛，主要的操作只用键盘也走得通。
 */

const SOURCES = {
  sources: [
    { id: 'src-a', name: '源甲', homepage: '', enabled: true, capabilities: { seeders: true, priority: 1 }, hasSelfTest: true },
    { id: 'src-b', name: '源乙', homepage: '', enabled: true, capabilities: { seeders: false, priority: 2 }, hasSelfTest: false },
  ],
  rules: { remoteUrl: 'https://rules.example.invalid/index.json', localDir: '', dir: '/rules', loaded: 2, errors: [], lastLoadedAt: Math.floor(Date.now() / 1000), lastSyncAt: null },
}

const MAGNET = 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567'
const RESULT = {
  query: '星屑',
  items: [
    { title: '[TestSub] Hoshikuzu no Tabibito [01-12][1080p]', magnet: MAGNET, size: '6.2 GB', fansub: 'TestSub', date: '2026-10-30', source: 'src-a', seeders: 18 },
    { title: '[OtherSub] 星屑的旅人 第02集 [1080p]', magnet: 'magnet:?xt=urn:btih:89abcdef0123456789abcdef0123456789abcdef', size: '512 MB', fansub: null, date: null, source: 'src-b' },
  ],
  sources: [
    { source: 'src-a', state: 'ok', count: 1, rawCount: 1, dropped: 0, latencyMs: 120 },
    { source: 'src-b', state: 'dead', count: 0, rawCount: 4, dropped: 4, reason: '规则解析不出任何条目', latencyMs: 80 },
  ],
}

for (const width of WIDTHS) {
  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height: VIEWPORT_HEIGHT } })

    test('磁力搜索有结果：结果表、来源状态都在，不溢出，整页无 serious / critical 问题', async ({ page }) => {
      await stubJson(page, 'GET', '/api/sources', SOURCES)
      await stubJson(page, 'GET', '/api/search', RESULT)
      const problems = watchProblems(page)
      await openApp(page, '/search?q=星屑')
      await expect(page.getByRole('table')).toBeVisible()
      await expect(page.getByRole('status').filter({ hasText: '2 条 · 1 个源正常 · 1 个源异常' })).toBeVisible()
      expect(await horizontalOverflow(page)).toBeLessThanOrEqual(0)
      expect(await blockingViolations(page)).toEqual([])
      expect(problems.errors).toEqual([])
    })

    test('磁力任务有会话、有下载：进度条有名字，整页无 serious / critical 问题', async ({ page }) => {
      await stubJson(page, 'GET', '/api/torrent/status', { active: true, phase: 'ready', name: '[TestSub] Hoshikuzu no Tabibito [01-12]', fileName: 'Hoshikuzu no Tabibito - 02 [1080p].mkv', peers: 6, seeders: 3, downRate: 2_400_000, upRate: 120_000, buffered: 1, progress: 0.42, cacheBytes: 812_000_000, seeding: false })
      await stubJson(page, 'GET', '/api/downloads', {
        dir: '/Users/you/Downloads/nagare',
        downloads: [
          { id: 'a'.repeat(40), title: '[TestSub] Hoshikuzu no Tabibito [01-12]', name: '星屑的旅人', root: '/dl', state: 'downloading', addedAt: 1, size: 6_000_000_000, bytesDone: 1_500_000_000, peers: 9, seeders: 4, downRate: 3_000_000 },
          { id: 'b'.repeat(40), title: '[OtherSub] 剧场版', root: '/dl', state: 'done', addedAt: 1, size: 3_000_000_000, bytesDone: 3_000_000_000, peers: 0, seeders: 0, downRate: 0, paths: ['/Users/you/Downloads/nagare/剧场版.mkv'] },
        ],
      })
      const problems = watchProblems(page)
      await openApp(page, '/torrents')
      await expect(page.getByRole('progressbar', { name: '下载进度', exact: true })).toHaveAttribute('aria-valuenow', '42')
      await expect(page.getByRole('progressbar', { name: '星屑的旅人 下载进度' })).toBeVisible()
      expect(await horizontalOverflow(page)).toBeLessThanOrEqual(0)
      expect(await blockingViolations(page)).toEqual([])
      expect(problems.errors).toEqual([])
    })
  })
}

test.describe('宽屏（1440px）', () => {
  test.use({ viewport: { width: 1440, height: VIEWPORT_HEIGHT } })

  test('搜索只用键盘：输入回车出结果；「播放」要选集时弹出选集，Esc 放弃后焦点回到那一行的播放按钮', async ({ page }) => {
    await stubJson(page, 'GET', '/api/sources', SOURCES)
    await stubJson(page, 'GET', '/api/search', RESULT)
    await stubJson(page, 'POST', '/api/torrent/play', {
      needSelection: true,
      files: [
        { index: 0, name: 'Hoshikuzu no Tabibito - 01 [1080p].mkv', path: 'Hoshikuzu/01.mkv', sizeBytes: 1_073_741_824, episode: 1 },
        { index: 1, name: 'NCOP.mkv', path: 'Hoshikuzu/Extras/NCOP.mkv', sizeBytes: 67_108_864, episode: null },
      ],
    })
    let stopped = 0
    await stubJson(page, 'POST', '/api/torrent/stop', () => {
      stopped += 1
      return {}
    })
    const problems = watchProblems(page)
    await openApp(page, '/search')

    // 没有关键词时输入框自己拿到焦点
    const input = page.getByRole('textbox', { name: '搜索关键词' })
    await expect(input).toBeFocused()
    await page.keyboard.type('星屑')
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL((url) => url.searchParams.get('q') === '星屑')
    await expect(page.getByRole('table')).toBeVisible()

    const play = page.getByRole('button', { name: /^播放 \[TestSub\]/ })
    await play.focus()
    await page.keyboard.press('Enter')
    const dialog = page.getByRole('dialog', { name: '选择要播放的剧集' })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByRole('button', { name: /^播放 01/ })).toBeFocused()
    expect(await blockingViolations(page), '选集弹窗打开时').toEqual([])

    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    expect(stopped).toBe(1)
    await expect(play).toBeFocused()
    expect(problems.errors).toEqual([])
  })

  test('「作品 / 磁力」切换带着同一个词：作品名搜索落到发现页，那边也能回到磁力搜索', async ({ page }) => {
    await stubJson(page, 'GET', '/api/sources', SOURCES)
    await stubJson(page, 'GET', '/api/search', RESULT)
    const problems = watchProblems(page)
    await openApp(page, '/search?q=星屑')
    const scope = page.getByRole('navigation', { name: '搜索范围' })
    await expect(scope.getByRole('link', { name: '磁力' })).toHaveAttribute('aria-current', 'page')
    await scope.getByRole('link', { name: '作品' }).click()
    await expect(page).toHaveURL((url) => url.pathname === '/' && url.searchParams.get('q') === '星屑')
    await expect(page.getByRole('heading', { name: '搜索「星屑」' })).toBeVisible()
    await page.getByRole('link', { name: /^在磁力来源里搜/ }).click()
    await expect(page).toHaveURL((url) => url.pathname === '/search' && url.searchParams.get('q') === '星屑')
    expect(problems.errors).toEqual([])
  })

  test('磁力搜索结果：整张表只有一个「已复制」播报区，复制哪一行就念哪一行', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await stubJson(page, 'GET', '/api/sources', SOURCES)
    await stubJson(page, 'GET', '/api/search', RESULT)
    await openApp(page, '/search?q=星屑')
    const table = page.locator('.res-wrap')
    await expect(page.getByRole('table')).toBeVisible()
    await table.getByRole('button', { name: /^复制磁力 \[OtherSub\]/ }).click()
    await expect(table.getByRole('status')).toHaveText(`已复制磁力：${RESULT.items[1]!.title}`)
    await table.getByRole('button', { name: /^复制磁力 \[TestSub\]/ }).click()
    await expect(table.getByRole('status')).toHaveCount(1)
    await expect(table.getByRole('status')).toHaveText(`已复制磁力：${RESULT.items[0]!.title}`)
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(MAGNET)
  })

  test('磁力任务读不出状态：错误说在一直挂着的播报区里（同一个元素，不是跟着错误一起插进来）', async ({ page }) => {
    let failing = false
    await page.route((url) => url.pathname === '/api/torrent/status', async (route) => {
      if (failing) await route.fulfill({ status: 503, json: { success: false, data: null, error: '磁力引擎没有启动' } })
      else await route.fulfill({ json: { success: true, data: { active: false } } })
    })
    await openApp(page, '/torrents')
    const streaming = page.getByRole('region', { name: '边下边播' })
    const live = streaming.getByRole('status')
    await expect(live).toHaveCount(1)
    await expect(live).toHaveText('')
    await live.evaluate((el) => { el.setAttribute('data-smoke-mark', '1') })
    failing = true
    await expect(live).toHaveText('磁力引擎没有启动')
    await expect(live).toHaveAttribute('data-smoke-mark', '1')
    expect(await blockingViolations(page)).toEqual([])
  })

  test('同步横幅：点「重试」补发一轮，结果说在状态行上，补上之后横幅消失', async ({ page }) => {
    let synced = false
    await stubJson(page, 'GET', '/api/player/status', () =>
      synced
        ? { playing: false }
        : { playing: false, sync: { state: 'failed', title: '星屑的旅人', episode: 2, reason: '服务暂不可达', recovery: '确认网络后点「重试」；下次启动 nagare 时也会自动再试', pending: 2 } },
    )
    await stubJson(page, 'POST', '/api/player/sync/retry', () => {
      synced = true
      return { synced: 2, remaining: 0 }
    })
    const problems = watchProblems(page)
    await openApp(page, '/library')
    const banner = page.locator('.sync-banner')
    await expect(banner).toContainText('「星屑的旅人 第2集」看完了')
    expect(await blockingViolations(page), '带同步横幅的媒体库').toEqual([])

    await banner.getByRole('button', { name: '重试' }).click()
    await expect(page.getByRole('status').filter({ hasText: '补记上 2 集，都记到账号了' })).toBeVisible()
    await expect(banner).toHaveCount(0)
    expect(problems.errors).toEqual([])
  })

  test('新手引导（一个文件夹都没有）：四项准备、可用键盘打开「浏览…」，整页无 serious / critical 问题', async ({ page }) => {
    await stubJson(page, 'GET', '/api/library', { folders: [], clusters: [], continueWatching: [], scannedAt: null })
    await stubJson(page, 'GET', '/api/fs/dirs', { path: '', parent: '', dirs: [{ name: '主目录', path: '/Users/you' }, { name: '影片', path: '/Users/you/Movies' }], truncated: false })
    const problems = watchProblems(page)
    await openApp(page, '/library')
    const onboard = page.getByRole('region', { name: '欢迎使用 nagare' })
    await expect(onboard.getByRole('heading', { level: 3 })).toHaveText(['播放器 mpv', '本地文件夹', '磁力搜索', '账号'])
    expect(await blockingViolations(page), '新手引导').toEqual([])

    await onboard.getByRole('button', { name: '浏览…' }).focus()
    await page.keyboard.press('Enter')
    const browser = onboard.getByRole('group', { name: '选择文件夹' })
    await expect(browser.getByRole('button', { name: /^主目录/ })).toBeFocused()
    expect(await blockingViolations(page), '展开目录浏览时').toEqual([])
    await page.keyboard.press('Escape')
    await expect(browser).toHaveCount(0)
    await expect(onboard.getByRole('button', { name: '浏览…' })).toBeFocused()
    expect(problems.errors).toEqual([])
  })

  test('浏览器里播不了的文件：说明原因并给回 mpv 的路', async ({ page }) => {
    const problems = watchProblems(page)
    await openApp(page, '/watch/file-1')
    await expect(page.getByRole('heading', { level: 1, name: '浏览器内播放' })).toBeVisible()
    await expect(page.getByRole('heading', { level: 2, name: '这里播不了' })).toBeVisible()
    await expect(page.getByRole('link', { name: '回去用 mpv 播' })).toHaveAttribute('href', '/anime/stardust')
    expect(problems.errors).toEqual([])
  })

  test('没有访问凭证：整页说明怎么带凭证重新打开，无 serious / critical 问题', async ({ page }) => {
    // 不走 openApp：它会带上启动 token。这一页的接口本来就该是 401，不收集「失败的请求」
    await page.goto('/settings')
    await expect(page.getByRole('heading', { level: 1, name: '这个浏览器还没有访问凭证' })).toBeVisible()
    await expect(page.getByRole('main')).toHaveCount(1)
    expect(await blockingViolations(page)).toEqual([])
  })
})
