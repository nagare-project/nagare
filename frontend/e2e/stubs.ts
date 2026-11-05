import type { Page, Route } from '@playwright/test'

/**
 * 冒烟测试里改写个别接口的回答：在 tools/uismoke 的固定假数据之上，只为某一个用例换一种状态
 * （mpv 没装、同步失败、有搜索结果……）。按路径名匹配，查询参数不参与（`/api/search?q=…` 也算）。
 */

type Method = 'GET' | 'POST'

/** 用固定的 data 回答（信封与正式后端一致）；data 可以是函数，每次请求现算（状态随操作变化时用） */
export async function stubJson(page: Page, method: Method, path: string, data: unknown | (() => unknown)): Promise<void> {
  await page.route((url) => url.pathname === path, async (route: Route) => {
    if (route.request().method() !== method) {
      await route.fallback()
      return
    }
    const body = typeof data === 'function' ? (data as () => unknown)() : data
    await route.fulfill({ json: { success: true, data: body } })
  })
}

/** 取 uismoke 的真实假数据回答，改其中一处再交给页面 */
export async function patchData(page: Page, path: string, patch: (data: Record<string, unknown>) => Record<string, unknown>): Promise<void> {
  await page.route((url) => url.pathname === path, async (route: Route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    const response = await route.fetch()
    const envelope = (await response.json()) as { success: boolean; data: Record<string, unknown> }
    await route.fulfill({ response, json: { ...envelope, data: patch(envelope.data) } })
  })
}
