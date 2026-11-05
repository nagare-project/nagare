import { expect } from '@playwright/test'
import type { Page } from '@playwright/test'

/** 冒烟覆盖的三种宽度：宽屏桌面 / 抽屉断点以下的平板 / 手机 */
export const WIDTHS = [1440, 900, 390] as const
export const VIEWPORT_HEIGHT = 900

/** 每一条路由（含 /discover 别名）都要能打开；作品页与分组页用假数据里存在的 id */
export const ROUTES = [
  '/',
  '/discover',
  '/library',
  '/anime/stardust',
  // 没认出作品的本地分组：没有海报 hero，主按钮是「认作品」
  '/anime/unknown-folder',
  '/entry?id=1001',
  '/lists',
  '/schedule',
  '/seasonal',
  '/torrents',
  '/search',
  '/settings',
  '/extensions',
  '/auto-downloader',
  '/debrid',
  '/watch/file-1',
] as const

/** 页面里出过的问题：控制台错误、未捕获异常、失败的请求 */
export interface PageProblems {
  errors: string[]
}

/** 开始收集页面问题。要在第一次 goto 之前调用。 */
export function watchProblems(page: Page): PageProblems {
  const problems: PageProblems = { errors: [] }
  page.on('console', (msg) => {
    if (msg.type() === 'error') problems.errors.push(`console: ${msg.text()}`)
  })
  page.on('pageerror', (err) => problems.errors.push(`pageerror: ${err.message}`))
  page.on('response', (res) => {
    if (res.status() >= 400) problems.errors.push(`HTTP ${res.status()}: ${new URL(res.url()).pathname}`)
  })
  page.on('requestfailed', (req) => {
    // 页面切走时取消的请求（AbortController）不算失败
    const failure = req.failure()?.errorText ?? ''
    if (!failure.includes('ERR_ABORTED')) problems.errors.push(`request failed: ${new URL(req.url()).pathname} ${failure}`)
  })
  return problems
}

/**
 * 带启动 token 打开某一页（与正式版一样：前端读走 ?token= 并从地址栏抹掉）。
 * 每个用例是全新的浏览器上下文，所以每次都要带。
 */
export async function openApp(page: Page, path: string): Promise<void> {
  const token = process.env.NAGARE_SMOKE_TOKEN
  if (token === undefined) throw new Error('NAGARE_SMOKE_TOKEN 没有设置（应由 playwright.config.ts 生成）')
  const url = new URL(path, 'http://smoke.invalid')
  url.searchParams.set('token', token)
  await page.goto(`${url.pathname}${url.search}${url.hash}`)
  await expect(page.locator('#root')).not.toBeEmpty()
  // 等首屏的接口都回来：懒加载路由与数据请求落地之后再检查布局
  await page.waitForLoadState('networkidle')
}

/** 页面没有横向溢出（DESIGN.md：手机宽度不能横向滚动） */
export async function horizontalOverflow(page: Page): Promise<number> {
  return page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
}
