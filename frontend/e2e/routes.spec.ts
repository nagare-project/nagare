import { expect, test } from '@playwright/test'
import { ROUTES, VIEWPORT_HEIGHT, WIDTHS, horizontalOverflow, openApp, watchProblems } from './helpers'

// 每条路由 × 三种宽度：页面渲染出来、没有横向溢出、没有控制台错误与失败的请求
for (const width of WIDTHS) {
  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height: VIEWPORT_HEIGHT } })

    for (const route of ROUTES) {
      test(`${route} 能打开，布局不溢出，没有错误`, async ({ page }) => {
        const problems = watchProblems(page)
        await openApp(page, route)

        // landmarks 齐全且唯一（DESIGN.md「无障碍」）：一个 banner（顶栏）、一个 main
        await expect(page.getByRole('banner')).toHaveCount(1)
        await expect(page.getByRole('banner')).toBeVisible()
        await expect(page.getByRole('main')).toHaveCount(1)
        await expect(page.getByRole('main')).toBeVisible()
        expect(await horizontalOverflow(page), '页面出现了横向滚动').toBeLessThanOrEqual(0)
        expect(problems.errors).toEqual([])
      })
    }
  })
}
