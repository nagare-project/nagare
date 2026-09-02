// @vitest-environment jsdom
// 搜索页冒烟：真实路由（memory history）+ mock 掉 endpoints 层。
// 重点验三种空态与「源异常 ≠ 无结果」（决议 CQ3 的验收：规则改坏时界面显示「源异常」）。
import { act } from 'react'
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SearchResult, SourceOutcome, SourcesData } from '../lib/endpoints'
import { fetchSources, fetchUpdate, searchMagnets, setSourceEnabled } from '../lib/endpoints'
import { createAppRouter } from '../routes'
import { mount } from '../test/harness'
import { installLocalStorage } from '../test/storage'

vi.mock('../lib/endpoints', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/endpoints')>()),
  fetchSources: vi.fn(),
  fetchUpdate: vi.fn(),
  searchMagnets: vi.fn(),
  setSourceEnabled: vi.fn(),
}))

const RULES: SourcesData['rules'] = {
  remoteUrl: 'https://example.invalid/rules',
  localDir: '',
  dir: '/tmp/rules',
  loaded: 2,
  errors: [],
  lastLoadedAt: 1_756_500_000,
  lastSyncAt: null,
}

const TWO_SOURCES: SourcesData = {
  sources: [
    {
      id: 'src-a',
      name: '源甲',
      homepage: 'https://a.invalid',
      enabled: true,
      capabilities: { seeders: true, priority: 1 },
      hasSelfTest: true,
    },
    {
      id: 'src-b',
      name: '源乙',
      homepage: '',
      enabled: true,
      capabilities: { seeders: false, priority: 2 },
      hasSelfTest: false,
    },
  ],
  rules: RULES,
}

function outcome(source: string, overrides: Partial<SourceOutcome> = {}): SourceOutcome {
  return { source, state: 'ok', count: 0, rawCount: 0, dropped: 0, latencyMs: 50, ...overrides }
}

async function mountAt(path: string) {
  const router = createAppRouter(createMemoryHistory({ initialEntries: [path] }))
  const mounted = await mount(<RouterProvider router={router} />)
  // 路由懒加载 + 首屏数据请求：再让一轮微任务落定
  await act(async () => {})
  return { router, ...mounted }
}

beforeEach(() => {
  vi.stubGlobal('scrollTo', vi.fn())
  installLocalStorage()
  vi.mocked(fetchSources).mockResolvedValue(TWO_SOURCES)
  vi.mocked(fetchUpdate).mockResolvedValue({
    enabled: true,
    current: '0.1.0',
    latest: '',
    available: false,
    url: '',
    checkedAt: null,
    error: '',
  })
  vi.mocked(searchMagnets).mockResolvedValue({ query: '', items: [], sources: [] })
  vi.mocked(setSourceEnabled).mockResolvedValue(undefined)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('SearchPage（空态）', () => {
  it('未搜索：有规则、没 q → 引导输入关键词，不发搜索请求', async () => {
    const { container, unmount } = await mountAt('/search')
    expect(container.textContent).toContain('输入关键词开始搜索')
    expect(container.textContent).toContain('已加载 2 个源，2 个启用')
    expect(searchMagnets).not.toHaveBeenCalled()
    // 顶栏入口
    expect(container.querySelector('a[href="/"]')).not.toBeNull()
    expect(container.querySelector('a[href="/settings"]')).not.toBeNull()
    await unmount()
  })

  it('无规则：sources 为空 → 引导去设置页添加规则来源，并列出规则加载错误', async () => {
    vi.mocked(fetchSources).mockResolvedValue({
      sources: [],
      rules: { ...RULES, loaded: 0, errors: ['broken.yaml: 缺少 name 字段'] },
    })
    const { container, unmount } = await mountAt('/search?q=frieren')
    expect(container.textContent).toContain('还没有规则来源')
    expect(container.textContent).toContain('nagare 不内置任何磁力源')
    expect(container.querySelector('a.search-empty-link')?.getAttribute('href')).toBe('/settings')
    expect(container.querySelector('.rules-errors')?.textContent).toContain('broken.yaml: 缺少 name 字段')
    // 引导文案不得出现任何具体站点
    expect(container.textContent).not.toMatch(/https?:\/\//)
    await unmount()
  })

  it('搜了但为空：源状态条是主角 —— 规则改坏的源显示「源异常」而不是「无结果」', async () => {
    vi.mocked(searchMagnets).mockResolvedValue({
      query: 'frieren',
      items: [],
      sources: [
        outcome('src-a', { state: 'zero' }),
        outcome('src-b', {
          state: 'dead',
          rawCount: 25,
          dropped: 25,
          reason: '规则解析不出任何条目',
          detail: 'selector "item title" matched 0 nodes',
        }),
      ],
    })
    const { container, unmount } = await mountAt('/search?q=frieren')

    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith('frieren')
    const chips = container.querySelectorAll('.src-bar .src-chip')
    expect(chips).toHaveLength(2)

    const zeroChip = container.querySelector('.src-chip--zero')
    expect(zeroChip?.textContent).toContain('源甲')
    expect(zeroChip?.textContent).toContain('无结果')

    const deadChip = container.querySelector('.src-chip--dead')
    expect(deadChip?.textContent).toContain('源乙')
    expect(deadChip?.textContent).toContain('源异常')
    expect(deadChip?.textContent).not.toContain('无结果')
    expect(deadChip?.getAttribute('title')).toContain('规则解析不出任何条目')
    expect(deadChip?.getAttribute('title')).toContain('selector "item title" matched 0 nodes')

    // 空结果文案指向源问题，而不是「没资源」
    expect(container.textContent).toContain('没有结果，且有源出了问题')
    expect(container.textContent).toContain('1 个源异常')
    expect(container.querySelector('.res-table')).toBeNull()
    await unmount()
  })
})

describe('SearchPage（结果与交互）', () => {
  const RESULT: SearchResult = {
    query: 'frieren',
    items: [
      {
        title: '[Sakurato] Sousou no Frieren [01][1080p]',
        magnet: 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567',
        size: '1.5 GB',
        fansub: '桜都字幕组',
        date: '2026-08-30',
        source: 'src-a',
        seeders: 12,
      },
      {
        title: '[Sakurato] Sousou no Frieren [02][1080p]',
        magnet: 'magnet:?xt=urn:btih:89abcdef0123456789abcdef0123456789abcdef',
        size: '',
        fansub: null,
        date: null,
        source: 'src-b',
      },
    ],
    sources: [outcome('src-a', { count: 1, rawCount: 1 }), outcome('src-b', { count: 1, rawCount: 1 })],
  }

  it('有结果：渲染结果表、来源徽标用规则名、状态行给出摘要；URL 里的 q 回填输入框', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    const { container, unmount } = await mountAt('/search?q=frieren')

    expect((container.querySelector('input[name="q"]') as HTMLInputElement).value).toBe('frieren')
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2)
    expect(container.querySelector('th.res-seeders')).not.toBeNull()
    const badges = Array.from(container.querySelectorAll('.res-source .badge')).map((el) => el.textContent)
    expect(badges).toEqual(['源甲', '源乙'])
    expect(container.querySelector('.search-status')?.textContent).toBe('2 条 · 2 个源正常')
    // 播放按钮全部禁用
    const plays = Array.from(container.querySelectorAll('.res-play-wrap button')) as HTMLButtonElement[]
    expect(plays.every((button) => button.disabled)).toBe(true)
    await unmount()
  })

  it('点 chip 切换启用状态：调 enabled 接口后自动重搜', async () => {
    vi.mocked(searchMagnets).mockResolvedValue(RESULT)
    const { container, unmount } = await mountAt('/search?q=frieren')
    expect(searchMagnets).toHaveBeenCalledTimes(1)

    const chip = container.querySelector('.src-bar button.src-chip') as HTMLButtonElement
    expect(chip.getAttribute('aria-pressed')).toBe('true')
    await act(async () => {
      chip.click()
    })
    await act(async () => {})

    expect(setSourceEnabled).toHaveBeenCalledExactlyOnceWith('src-a', false)
    expect(fetchSources).toHaveBeenCalledTimes(2)
    expect(searchMagnets).toHaveBeenCalledTimes(2)
    expect(searchMagnets).toHaveBeenLastCalledWith('frieren')
    await unmount()
  })

  it('提交表单：关键词写进 URL ?q=，并触发搜索', async () => {
    const { container, router, unmount } = await mountAt('/search')
    const input = container.querySelector('input[name="q"]') as HTMLInputElement
    const form = container.querySelector('form[role="search"]') as HTMLFormElement

    await act(async () => {
      // React 受控输入：走原生 setter 再派发 input 事件
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      setter?.call(input, '  葬送的芙莉莲 ')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      form.requestSubmit()
    })
    await act(async () => {})

    expect(router.state.location.search).toEqual({ q: '葬送的芙莉莲' })
    expect(searchMagnets).toHaveBeenCalledExactlyOnceWith('葬送的芙莉莲')
    await unmount()
  })
})
