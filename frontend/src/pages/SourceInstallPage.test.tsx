// @vitest-environment jsdom
import { act } from 'react'
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '../routes'
import { mount } from '../test/harness'
import { installLocalStorage } from '../test/storage'
import { TOKEN_STORAGE_KEY } from '../lib/token'

const sha = 'ab'.repeat(32)
const sourceUrl = 'https://example.invalid/sources/demo.json'
const summary = {
  id: 'demo', name: '示例来源', kind: 'bt', tier: 2, version: '3', enabled: true,
  description: '只用于测试', ecosystem: 'test', upstream: 'https://example.invalid/upstream',
  license: 'MIT', allowedHosts: ['api.example.invalid'],
}
const preview = {
  url: sourceUrl, sha256: sha, bytes: 2048, format: 'json', content: '{"id":"demo"}',
  package: { id: 'demo', name: '示例来源', description: '只用于测试', version: '3', pack: false },
  sources: [{ source: summary }],
  conflicts: [],
}
const packUrl = 'https://example.invalid/packs/demo.json'
const packPreview = {
  url: packUrl, sha256: sha, bytes: 4096, format: 'json', content: '{"schema":"nagare-source-pack/v1"}',
  package: { id: 'demo-pack', name: '示例来源包', description: '两个来源', version: 'abc123', pack: true },
  sources: [
    { source: { ...summary, id: 'alpha', name: '甲来源', allowedHosts: ['alpha.example.invalid'] } },
    {
      source: { ...summary, id: 'beta', name: '乙来源', kind: 'web', tier: 1, allowedHosts: ['beta.example.invalid', 'cdn.example.invalid'] },
      existing: { id: 'beta', name: '旧乙来源', kind: 'web', tier: 1, version: '1', enabled: true, status: 'healthy', capabilities: [] },
    },
  ],
  conflicts: [],
}
const installedView = {
  config: { enabled: true, executable: '/opt/nagare-source', root: '' }, status: { phase: 'ready' },
  sources: [], installed: [], installSupported: true,
}

interface Call { path: string; method: string; body: unknown }
type Reply = { status: number; data?: unknown; error?: string }
let calls: Call[]
let replies: Record<string, Reply>

beforeEach(() => {
  installLocalStorage()
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, '00000000000000000000000000000000')
  vi.stubGlobal('scrollTo', vi.fn())
  calls = []
  replies = {
    'POST /api/source-plugin/sources/preview': { status: 200, data: preview },
    'POST /api/source-plugin/sources/install': { status: 200, data: installedView },
  }
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), 'http://localhost').pathname
    const method = init?.method ?? 'GET'
    calls.push({ path, method, body: typeof init?.body === 'string' ? JSON.parse(init.body) as unknown : undefined })
    const reply = replies[`${method} ${path}`] ?? { status: 404, error: 'not found' }
    const envelope = reply.status < 400 ? { success: true, data: reply.data } : { success: false, error: reply.error }
    return new Response(JSON.stringify(envelope), { status: reply.status, headers: { 'Content-Type': 'application/json' } })
  }))
})
afterEach(() => { vi.unstubAllGlobals(); window.sessionStorage.clear() })

async function open(path: string) {
  const router = createAppRouter(createMemoryHistory({ initialEntries: [path] }))
  await router.load()
  const mounted = await mount(<RouterProvider router={router} />)
  await settle()
  return { router, ...mounted }
}

async function settle() {
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)) })
}

function installLink(url = sourceUrl, digest = sha) {
  return `/sources/install?url=${encodeURIComponent(url)}&sha256=${digest}`
}

const button = (container: HTMLElement, text: string) =>
  [...container.querySelectorAll<HTMLButtonElement>('button')].find(item => item.textContent === text)
const posts = (path: string) => calls.filter(call => call.method === 'POST' && call.path === path)

describe('从地址安装来源', () => {
  it('打开链接只预览不安装，确认后按预览摘要安装', async () => {
    const { container, unmount } = await open(installLink())
    expect(posts('/api/source-plugin/sources/preview').map(call => call.body)).toEqual([{ url: sourceUrl, sha256: sha }])
    expect(posts('/api/source-plugin/sources/install')).toHaveLength(0)

    const text = container.textContent ?? ''
    expect(text).toContain('示例来源')
    expect(text).toContain('demo · v3')
    expect(text).toContain('api.example.invalid')
    expect(text).toContain(sha)
    expect(text).toContain('不审核也不推荐任何来源')
    expect(container.querySelector('details pre')?.textContent).toBe('{"id":"demo"}')

    await act(async () => { button(container, '安装')!.click() })
    await settle()
    expect(posts('/api/source-plugin/sources/install').map(call => call.body)).toEqual([{ url: sourceUrl, sha256: sha }])
    expect(container.querySelector('[role="status"]')?.textContent).toContain('已安装「示例来源」')
    await unmount()
  })

  it('没有摘要的链接也能预览，安装时提交插件校验过的摘要', async () => {
    const { container, unmount } = await open(`/sources/install?url=${encodeURIComponent(sourceUrl)}`)
    expect(posts('/api/source-plugin/sources/preview')[0]?.body).toEqual({ url: sourceUrl, sha256: '' })
    await act(async () => { button(container, '安装')!.click() })
    await settle()
    expect(posts('/api/source-plugin/sources/install')[0]?.body).toEqual({ url: sourceUrl, sha256: sha })
    await unmount()
  })

  it('摘要不符时显示后端原因且不出现安装按钮', async () => {
    replies['POST /api/source-plugin/sources/preview'] = { status: 400, error: '来源文件的摘要与链接给出的不符' }
    const { container, unmount } = await open(installLink())
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('摘要与链接给出的不符')
    expect(button(container, '安装')).toBeUndefined()
    expect(container.textContent).not.toContain('先在设置里启用')
    await unmount()
  })

  it('插件不可用（503）时引导去来源设置，重试会重新预览', async () => {
    replies['POST /api/source-plugin/sources/preview'] = { status: 503, error: '来源插件尚未就绪' }
    const { container, unmount } = await open(installLink())
    expect(container.textContent).toContain('来源插件尚未就绪')
    expect(container.textContent).toContain('先在设置里启用 Nagare Source')
    expect(container.querySelector('a[href="/settings#sources"]')).not.toBeNull()

    replies['POST /api/source-plugin/sources/preview'] = { status: 200, data: preview }
    await act(async () => { button(container, '重试')!.click() })
    await settle()
    expect(posts('/api/source-plugin/sources/preview')).toHaveLength(2)
    expect(container.textContent).toContain('示例来源')
    await unmount()
  })

  it('安装失败时保留确认页并显示原因', async () => {
    replies['POST /api/source-plugin/sources/install'] = { status: 502, error: '来源插件重启失败，已撤销安装' }
    const { container, unmount } = await open(installLink())
    await act(async () => { button(container, '安装')!.click() })
    await settle()
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('已撤销安装')
    expect(button(container, '安装')?.disabled).toBe(false)
    await unmount()
  })

  it('文件标记为停用时禁止安装', async () => {
    replies['POST /api/source-plugin/sources/preview'] = { status: 200, data: { ...preview, sources: [{ source: { ...summary, enabled: false } }] } }
    const { container, unmount } = await open(installLink())
    expect(container.textContent).toContain('标记为停用')
    expect(button(container, '安装')?.disabled).toBe(true)
    await unmount()
  })

  it('已安装旧版本时说明会替换，按钮改为更新', async () => {
    const installed = {
      id: 'demo', name: '示例来源', version: '2', pack: false, url: 'https://old.example.invalid/demo.json', sha256: 'cd'.repeat(32),
      installedAt: '2026-01-02T03:04:05Z', sources: [{ id: 'demo', name: '示例来源', kind: 'bt', version: '2', format: 'json' }],
    }
    replies['POST /api/source-plugin/sources/preview'] = { status: 200, data: { ...preview, installed } }
    const { container, unmount } = await open(installLink())
    expect(container.textContent).toContain('已安装 v2（来自 old.example.invalid），确认后将替换为这个文件')
    expect(button(container, '更新')).toBeDefined()
    await unmount()
  })

  it('来源包列出每个来源，确认一次整体安装', async () => {
    replies['POST /api/source-plugin/sources/preview'] = { status: 200, data: packPreview }
    const { container, unmount } = await open(installLink(packUrl))
    expect(container.querySelector('h2')?.textContent).toBe('示例来源包')
    const text = container.textContent ?? ''
    expect(text).toContain('来源包 · 2 个来源 · vabc123')
    expect(text).toContain('不审核也不推荐任何来源')
    expect(text).toContain('需要本机安装 Chrome 或 Chromium')
    const members = [...container.querySelectorAll('.source-install-members > li')].map(item => item.textContent ?? '')
    expect(members).toHaveLength(2)
    expect(members[0]).toContain('甲来源')
    expect(members[0]).toContain('BT 磁力 · 第 2 层')
    expect(members[0]).toContain('alpha.example.invalid')
    expect(members[1]).toContain('在线播放 · 第 1 层')
    expect(members[1]).toContain('cdn.example.invalid')
    expect(members[1]).toContain('插件当前已有同 id 的来源「旧乙来源」')

    await act(async () => { button(container, '安装')!.click() })
    await settle()
    expect(posts('/api/source-plugin/sources/install').map(call => call.body)).toEqual([{ url: packUrl, sha256: sha }])
    expect(container.querySelector('[role="status"]')?.textContent).toContain('已安装「示例来源包」')
    await unmount()
  })

  it('来源已由别的安装项提供时列出冲突并禁止安装', async () => {
    const conflicts = [{ sourceId: 'alpha', packageId: 'alpha', packageName: '单独装的甲' }]
    replies['POST /api/source-plugin/sources/preview'] = { status: 200, data: { ...packPreview, conflicts } }
    const { container, unmount } = await open(installLink(packUrl))
    const alert = container.querySelector('[role="alert"]')?.textContent ?? ''
    expect(alert).toContain('请先在来源设置里卸载')
    expect(alert).toContain('alpha（「单独装的甲」）')
    expect(button(container, '安装')?.disabled).toBe(true)
    await unmount()
  })

  it('更新来源包时说明会移除新版本不再包含的来源', async () => {
    const installed = {
      id: 'demo-pack', name: '示例来源包', version: 'old', pack: true, url: packUrl, sha256: 'cd'.repeat(32), installedAt: '2026-01-02T03:04:05Z',
      sources: [
        { id: 'alpha', name: '甲来源', kind: 'bt', version: '1', format: 'json' },
        { id: 'gamma', name: '丙来源', kind: 'bt', version: '1', format: 'json' },
      ],
    }
    replies['POST /api/source-plugin/sources/preview'] = { status: 200, data: { ...packPreview, installed } }
    const { container, unmount } = await open(installLink(packUrl))
    expect(container.textContent).toContain('已安装 vold（来自 example.invalid），确认后将替换为这个文件')
    expect(container.textContent).toContain('新版本不再包含 丙来源，会一并移除')
    expect(button(container, '更新')?.disabled).toBe(false)
    await unmount()
  })

  it('来源包里有停用的来源时点名并禁止安装', async () => {
    const sources = [packPreview.sources[0]!, { source: { ...summary, id: 'gamma', enabled: false } }]
    replies['POST /api/source-plugin/sources/preview'] = { status: 200, data: { ...packPreview, sources } }
    const { container, unmount } = await open(installLink(packUrl))
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('来源 gamma 在文件里标记为停用')
    expect(button(container, '安装')?.disabled).toBe(true)
    await unmount()
  })

  it('浏览器没有凭证时显示未授权提示', async () => {
    replies['POST /api/source-plugin/sources/preview'] = { status: 401, error: '缺少有效 token' }
    const { container, unmount } = await open(installLink())
    expect(container.textContent).toContain('这个浏览器还没有访问凭证')
    expect(container.querySelectorAll('main')).toHaveLength(1)
    await unmount()
  })

  it('不带地址打开时不发请求，填写后跳到预览', async () => {
    const { container, router, unmount } = await open('/sources/install')
    expect(calls.filter(call => call.path.startsWith('/api/source-plugin'))).toHaveLength(0)
    const input = container.querySelector<HTMLInputElement>('#source-install-url')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, ` ${sourceUrl} `)
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => { container.querySelector<HTMLFormElement>('.source-install-form')!.requestSubmit() })
    await settle()
    expect(router.state.location.search).toEqual({ url: sourceUrl })
    expect(posts('/api/source-plugin/sources/preview')[0]?.body).toEqual({ url: sourceUrl, sha256: '' })
    expect(container.textContent).toContain('示例来源')
    await unmount()
  })
})
