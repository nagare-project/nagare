import { randomBytes } from 'node:crypto'
import { defineConfig, devices } from '@playwright/test'

/**
 * 界面冒烟测试（R10）：真实浏览器（只用 Chromium —— 维护者的 Mac 上 WebKit 自动化起不来）
 * 打开构建好的界面，后端是 tools/uismoke：真实的鉴权 / CSP / SPA 回落 + 固定假数据，不连网络。
 *
 * 先构建前端（bun run build 落到 ../web），再 bun run test:e2e。
 */

const port = Number(process.env.NAGARE_SMOKE_PORT ?? 8599)
// token 每次运行随机生成；只在主进程生成一次，worker 进程从环境变量继承同一个
process.env.NAGARE_SMOKE_TOKEN ??= randomBytes(16).toString('hex')

const binary = 'node_modules/.cache/uismoke/uismoke'

export default defineConfig({
  testDir: './e2e',
  // 每个用例开自己的页面，互不共享状态；本机核心数足够时并行
  fullyParallel: true,
  forbidOnly: process.env.CI !== undefined,
  retries: process.env.CI !== undefined ? 1 : 0,
  reporter: process.env.CI !== undefined ? [['github'], ['list']] : 'list',
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    // 系统偏好对页面有影响（动画、配色）：钉住，结果不随机器变
    reducedMotion: 'no-preference',
    colorScheme: 'dark',
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    // 先编译再 exec：直接 go run 的话，Playwright 结束时只杀掉 go 命令本身，真正监听端口的子进程会留下来
    command: `go build -o ${binary} ../tools/uismoke && exec ${binary} -web ../web -fixtures e2e/fixtures -port ${port}`,
    url: `http://127.0.0.1:${port}/`,
    env: { CGO_ENABLED: '0', NAGARE_SMOKE_TOKEN: process.env.NAGARE_SMOKE_TOKEN },
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
})
