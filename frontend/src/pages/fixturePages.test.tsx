// @vitest-environment jsdom
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { act } from 'react'
import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { getAllByRole, getAllByText, getByRole } from '../test/queries'
import { mount } from '../test/harness'
import { installLocalStorage } from '../test/storage'
import { DiscoverPage } from './DiscoverPage'
import { EMPTY_LIBRARY_DISMISS_KEY } from '../components/catalog/EmptyLibraryStrip'
import { ListsPage } from './ListsPage'
import { SchedulePage } from './SchedulePage'
import { MediaCard } from '../components/media/MediaCard'
import { FAKE_LISTS } from '../lib/fixtures/library'
import { fetchCatalogSearch } from '../lib/media'
import { fetchDiscoverData, fetchLibrary } from '../lib/endpoints'
import type { DiscoverData, LibraryData } from '../lib/endpoints'
import { smokeDiscover } from '../test/discoverFixtures'

// 发现页读 /discover 的地址栏参数（?q= 作品搜索）。这组测试不挂路由，用可控的替身代替
let discoverSearch: { q?: string; genre?: string } = {}
vi.mock('@tanstack/react-router', async importOriginal => {
  const original = await importOriginal<typeof import('@tanstack/react-router')>()
  return { ...original, useSearch: () => discoverSearch, useNavigate: () => () => Promise.resolve() }
})

vi.mock('../lib/media', async importOriginal => {
  const original = await importOriginal<typeof import('../lib/media')>()
  const { FAKE_DISCOVER } = await import('../lib/fixtures/library')
  return { ...original, fetchCatalogSearch: vi.fn(async (q: string) => q === '没有这部' ? [] : [FAKE_DISCOVER.trending[0]!]) }
})

vi.mock('../lib/endpoints', async importOriginal => {
  const original = await importOriginal<typeof import('../lib/endpoints')>()
  const { fakeSchedule } = await import('../lib/fixtures/schedule')
  // 默认媒体库里有东西（发现页不出「去添加」那一条）；要测空库时单次覆盖
  const library = { folders: [{ id: 'f1', path: '/番剧', addedAt: 1 }], clusters: [{ clusterKey: 'k', title: '作品', season: null, confidence: 1, episodeCount: 1, groups: [] }], continueWatching: [], scannedAt: 1 }
  const { smokeDiscover } = await import('../test/discoverFixtures')
  return { ...original, fetchLibrary: vi.fn(async () => library), fetchDiscoverData: vi.fn(async () => smokeDiscover()), fetchSchedule: vi.fn(async () => ({ fetchedAt: Date.now() / 1000, airings: fakeSchedule().events.map(e => ({
    anilistId: e.media.id, episode: e.episode, airingAt: Date.parse(e.airingAt) / 1000,
    title: e.media.title, cover: e.media.cover, format: e.media.format, inLibrary: false,
  })) })) }
})

// 生产页面使用真实接口；这里仅用可重复的接口替身验证展示与交互。

/**
 * 页面模块表。用 import.meta.glob 而不是拼出来的 `import(\`./${x}\`)`：
 * 后者 Vite 静态分析不了（会告警），能不能加载全看运行时凑巧。
 */
const PAGE_MODULES = import.meta.glob('./*.tsx') as Record<string, () => Promise<unknown>>

/** 发现页的各段：用标题命名的 section（aria-labelledby 指向段标题） */
function discoverSections(container: HTMLElement): HTMLElement[] {
  return [...container.querySelectorAll<HTMLElement>('section[aria-labelledby]')]
}
function sectionTitle(section: HTMLElement): string | null | undefined {
  return section.ownerDocument.getElementById(section.getAttribute('aria-labelledby')!)?.textContent
}
/** 一份发现页数据，改几块 */
function withDiscover(patch: (data: DiscoverData) => DiscoverData) {
  vi.mocked(fetchDiscoverData).mockResolvedValueOnce(patch(smokeDiscover()))
}

describe('假数据页面', () => {
  it('我的追番：未登录时提供账号入口，不展示演示收藏', async () => {
    const { container, unmount } = await mount(<ListsPage />)
    expect(container.querySelector('h1')?.textContent).toBe('我的追番')
    expect(container.textContent).toContain('登录后显示你的追番')
    expect(container.querySelectorAll('li')).toHaveLength(0)
    const login = [...container.querySelectorAll('a')].find(a => a.textContent === '去登录')
    expect(login?.getAttribute('href')).toBe('/settings#account')
    await unmount()
  })

  it('发现：页面有一个 h1（视觉上不显示）；关掉空库说明后焦点落在它上面', async () => {
    installLocalStorage()
    vi.mocked(fetchLibrary).mockResolvedValueOnce({ folders: [], clusters: [], continueWatching: [], scannedAt: null } as LibraryData)
    const { container, unmount } = await mount(<DiscoverPage />)
    const title = getByRole(container, 'heading', { level: 1, name: '发现' })
    expect(title.className).toContain('visually-hidden')
    const close = getByRole(container, 'button', { name: '关闭这条说明' })
    close.focus()
    await act(async () => close.click())
    expect(document.activeElement).toBe(title)
    await unmount()
  })

  it('发现：横幅、今日更新、本季高分、按色调逛、大家都在追、完结佳作、年度榜，作品都链到作品页', async () => {
    const { container, unmount } = await mount(<DiscoverPage />)
    expect(getByRole(container, 'region', { name: '本季焦点' })).toBeDefined()
    const sections = discoverSections(container)
    expect(sections.map(sectionTitle)).toEqual(['今日更新', '本季高分', '按色调逛', '大家都在追', '完结佳作', '2026 年度榜'])
    for (const section of sections) {
      expect(section.querySelectorAll('a[href^="/entry?id="]').length, sectionTitle(section) ?? '').toBeGreaterThan(0)
    }
    // 本季全部去季度页；今日更新去放送表
    expect([...container.querySelectorAll('a')].filter(a => a.textContent === '本季全部 14 部').map(a => a.getAttribute('href'))).toEqual(['/seasonal', '/seasonal'])
    expect([...container.querySelectorAll('a')].find(a => a.textContent === '完整放送表')?.getAttribute('href')).toBe('/schedule')
    await unmount()
  })

  it('发现：上游整段为空、或读取失败的板块整块隐藏，原因不写在页面上', async () => {
    withDiscover(data => ({ ...data, trending: { items: [], error: '上游超时' }, gems: { items: [] }, schedule: { items: [], error: '放送表超时' } }))
    const { container, unmount } = await mount(<DiscoverPage />)
    expect(discoverSections(container).map(sectionTitle)).toEqual(['本季高分', '按色调逛', '2026 年度榜'])
    expect(container.textContent).not.toContain('超时')
    await unmount()
  })

  it('发现：离线时在横幅上说「离线 · 更新于 …」，内容照常显示', async () => {
    withDiscover(data => ({ ...data, freshness: { fetchedAt: new Date(Date.now() - 2 * 3600_000).toISOString(), stale: false, offline: true } }))
    const { container, unmount } = await mount(<DiscoverPage />)
    // 看得见的一句，与读屏在进入离线时听到的那一次
    expect(getAllByText(container, '离线 · 更新于 2 小时前')).toHaveLength(2)
    expect(getAllByRole(container, 'status').filter(el => el.textContent?.startsWith('离线')).map(el => el.textContent)).toEqual(['离线 · 更新于 2 小时前'])
    expect(discoverSections(container).length).toBeGreaterThan(0)
    await unmount()
  })

  it('发现：每一块都读不到、盘上也没有时，整页说明「媒体库照常可用」', async () => {
    withDiscover(data => ({ ...data, seasonal: { items: [], error: 'x' }, trending: { items: [], error: 'x' }, gems: { items: [], error: 'x' }, yearly: { items: [], error: 'x' }, schedule: { items: [], error: 'x' } }))
    const { container, unmount } = await mount(<DiscoverPage />)
    expect(getByRole(container, 'heading', { level: 2, name: '连不上目录服务，媒体库照常可用' })).toBeDefined()
    await unmount()
  })

  it('发现：一份缓存都没有又连不上时，整页说明「媒体库照常可用」，给去媒体库与重试', async () => {
    vi.mocked(fetchDiscoverData).mockRejectedValueOnce(new Error('无法连接到目录服务'))
    const { container, unmount } = await mount(<DiscoverPage />)
    // 标题仍是标题；播报包着标题与原因一起念
    expect(getByRole(container, 'heading', { level: 2, name: '连不上目录服务，媒体库照常可用' })).toBeDefined()
    expect(getByRole(container, 'alert').textContent).toBe('连不上目录服务，媒体库照常可用无法连接到目录服务')
    expect([...container.querySelectorAll('a')].find(a => a.textContent === '去媒体库')?.getAttribute('href')).toBe('/library')
    const retry = [...container.querySelectorAll('button')].find(b => b.textContent === '重试')!
    await act(async () => retry.click())
    await act(async () => { await Promise.resolve() })
    expect(discoverSections(container).length).toBeGreaterThan(0)
    await unmount()
  })

  it('发现：媒体库是空的时顶部一行「去添加」，关掉后不再出现', async () => {
    installLocalStorage()
    vi.mocked(fetchLibrary).mockResolvedValueOnce({ folders: [], clusters: [], continueWatching: [], scannedAt: null } as LibraryData)
    const { container, unmount } = await mount(<DiscoverPage />)
    expect(container.textContent).toContain('nagare 播放你自己的番剧文件')
    expect([...container.querySelectorAll('a')].find(a => a.textContent === '去添加')?.getAttribute('href')).toBe('/library')
    await act(async () => [...container.querySelectorAll('button')].find(b => b.getAttribute('aria-label') === '关闭这条说明')!.click())
    expect(container.textContent).not.toContain('nagare 播放你自己的番剧文件')
    expect(window.localStorage.getItem(EMPTY_LIBRARY_DISMISS_KEY)).toBe('1')
    await unmount()
  })

  it('发现：带关键词时只显示作品搜索结果，榜单让位', async () => {
    discoverSearch = { q: '芙莉莲' }
    try {
      const { container, unmount } = await mount(<DiscoverPage />)
      await act(async () => { await Promise.resolve() })
      expect(container.textContent).toContain('搜索「芙莉莲」')
      expect(vi.mocked(fetchCatalogSearch)).toHaveBeenCalledWith('芙莉莲', expect.anything())
      expect(container.querySelectorAll('.catalog-search-results li a[href^="/entry?id="]').length).toBeGreaterThan(0)
      // 发现页的各段让位
      expect(container.textContent).not.toContain('大家都在追')
      expect(container.querySelector('[aria-label="本季焦点"]')).toBeNull()
      await unmount()
    } finally {
      discoverSearch = {}
    }
  })

  it('发现：搜不到时说清楚，并提示换个叫法', async () => {
    discoverSearch = { q: '没有这部' }
    try {
      const { container, unmount } = await mount(<DiscoverPage />)
      await act(async () => { await Promise.resolve() })
      expect(container.textContent).toContain('没有找到「没有这部」相关的作品')
      await unmount()
    } finally {
      discoverSearch = {}
    }
  })

  it('发现：横幅是本季前五、第一张有颜色；点短条立刻换，整页跟着换颜色', async () => {
    const { container, unmount } = await mount(<DiscoverPage />)
    const hero = getByRole(container, 'region', { name: '本季焦点' })
    const bars = [...hero.querySelectorAll<HTMLButtonElement>('button[aria-pressed]')]
    expect(bars).toHaveLength(5)
    expect(bars.map(b => b.getAttribute('aria-pressed'))).toEqual(['true', 'false', 'false', 'false', 'false'])
    const main = container.querySelector('main')!
    const before = main.style.getPropertyValue('--home-ground')
    expect(before).toMatch(/^oklch\(13% 0\.012 [\d.]+\)$/)
    // 预告片不在发现页
    expect(container.querySelector('iframe')).toBeNull()
    await act(async () => bars[2]!.click())
    expect(bars[2]!.getAttribute('aria-pressed')).toBe('true')
    expect(main.style.getPropertyValue('--home-ground')).not.toBe(before)
    // 不在焦点上的几张对读屏隐藏、不能聚焦
    const slides = [...hero.querySelectorAll('article')]
    expect(slides.filter(a => a.getAttribute('aria-hidden') === 'false')).toHaveLength(1)
    await unmount()
  })

  it('发现：不再内嵌放送表（放送表在顶栏有自己的一页）', async () => {
    const { container, unmount } = await mount(<DiscoverPage />)
    expect(container.querySelector('[role="tablist"]')).toBeNull()
    expect(container.querySelector('.schedule-agenda')).toBeNull()
    await unmount()
  })

  it('放送表：月历按整周排列，今天有标记', async () => {
    const { container, unmount } = await mount(<SchedulePage />)
    const count = container.querySelectorAll('.calendar-day').length
    expect(count % 7).toBe(0)
    expect(count).toBeGreaterThanOrEqual(28)
    // 「今天」有且只有一个 —— 多于一个说明周几的换算错了
    expect(container.querySelectorAll('.calendar-day--today')).toHaveLength(1)
    expect(container.querySelector('[aria-current="date"]')).not.toBeNull()
    // 至少排了几场
    expect(container.querySelectorAll('.calendar-event').length).toBeGreaterThan(3)
    expect(container.textContent).not.toContain('演示追番列表')
    await unmount()
  })

  /**
   * 名单是【从 import 里推出来的】，不是手写的。
   *
   * 手写名单的失效方式很具体：有人新建一个吃 fixture 的页面，忘了加横幅，
   * 也忘了往这个名单里加一行 —— 于是一屏编出来的数字被当成真的发出去，
   * 而 379 个测试全绿。改成从文件系统推导之后，新页面自动进入这条断言。
   */
  it('每个吃假数据的页面都必须挂着横幅，不许把假数据伪装成真的', async () => {
    // 不能用 import.meta.url：jsdom 环境下它是相对文档 base 解析的，
    // 得到 /src/pages 这种假路径。vitest 的 cwd 就是 frontend/。
    const pagesDir = join(process.cwd(), 'src/pages')
    const fixturePages = readdirSync(pagesDir)
      .filter((f) => f.endsWith('.tsx') && !f.endsWith('.test.tsx'))
      .filter((f) => readFileSync(join(pagesDir, f), 'utf8').includes('lib/fixtures'))
      .sort()

    // 扫描器活着才算数：路径变了会让这条静默地变成「零个页面、零个问题」
    expect(fixturePages).toEqual(['AutoDownloaderPage.tsx'])

    for (const file of fixturePages) {
      const name = file.replace(/\.tsx$/, '')
      const load = PAGE_MODULES[`./${file}`]
      expect(load, `${file} 不在 import.meta.glob 的范围里`).toBeTypeOf('function')
      const mod = (await load!()) as Record<string, unknown>
      // 按【文件名】取导出，不是「第一个函数导出」：ListsPage 同时导出
      // ListsPage 与 FixtureNotice，取第一个就成了看导出顺序的运气。
      const Page = mod[name] as () => ReactElement
      expect(Page, `${file} 没有同名导出`).toBeTypeOf('function')
      const { container, unmount } = await mount(<Page />)
      const notice = container.querySelector('.alert-warn')
      expect(notice, `${file} 吃 fixture 却没有横幅`).not.toBeNull()
      const text = notice?.textContent ?? ''
      // 「假数据」或更重的措辞（自动下载页说的是「功能尚未实现」），
      // 外加缺口编号 —— 用户报问题时报得出「G2 那一页」比描述半天强。
      // 不断言指向任何文档：缺口清单是内部文件，公开仓库里没有。
      expect(text, `${file} 的横幅没说清这是假的`).toMatch(/假数据|尚未实现/)
      expect(text, `${file} 的横幅没带缺口编号`).toMatch(/G\d/)
      await unmount()
    }
  })
})

// 这里原来还有一条「扫描记录」的用例，断言界面上必须出现
// 「N 个文件解析不出集号，已跳过」。那句话是错的：items.go:44-80 的 BuildItems
// 只在非视频文件上 continue，集号解析失败时 Episode 是 nil，条目照样进库。
// 一条测试在钉一句错话 —— 那会让错误在重构里活下来。整页连同它一起删了，
// 真实的扫描丢弃改由 GET /api/library 的 folders[].dropped 提供。
describe('自动下载', () => {
  it('自动下载：措辞必须说清「点了也不会下载」，而不是只说数据是假的', async () => {
    const { AutoDownloaderPage } = await import('./AutoDownloaderPage')
    const { container, unmount } = await mount(<AutoDownloaderPage />)
    const notice = container.querySelector('.alert-warn')?.textContent ?? ''
    expect(notice).toContain('功能尚未实现')
    expect(notice).toContain('不会下载任何东西')
    await unmount()
  })

  it('自动下载：不预填任何可用的订阅地址（红线 1）', async () => {
    const { AutoDownloaderPage } = await import('./AutoDownloaderPage')
    const { container, unmount } = await mount(<AutoDownloaderPage />)
    const feeds = [...container.querySelectorAll('.rule-feed')].map((e) => e.textContent ?? '')
    expect(feeds.length).toBeGreaterThan(0)
    for (const f of feeds) {
      // 占位尖括号必须在：一个真能用的 RSS 地址等于官方分发源
      expect(f, `订阅地址不该是可用地址：${f}`).toContain('<')
    }
    await unmount()
  })

  it('自动下载：开关可切换（纯本地状态，不发请求）', async () => {
    const { AutoDownloaderPage } = await import('./AutoDownloaderPage')
    const { container, unmount } = await mount(<AutoDownloaderPage />)
    const sw = container.querySelector<HTMLButtonElement>('.switch')
    const before = sw?.getAttribute('aria-checked')
    await act(async () => sw?.click())
    expect(container.querySelector('.switch')?.getAttribute('aria-checked')).not.toBe(before)
    await unmount()
  })
})

describe('作品卡的信息分层', () => {
  it('重要信息常驻可见，只有简介收进 hover 浮层', async () => {
    // 这条守的是一条无障碍规则：hover 在触屏上根本不存在，所以评分、
    // 年份、集数、类型这些不能只在浮层里。哪天有人把它们挪进浮层，这里会红。
    const { container, unmount } = await mount(<ul>{FAKE_LISTS.watching.map(media => <MediaCard key={media.id} media={media} />)}</ul>)
    const card = container.querySelector('.poster')

    expect(card?.querySelector('.poster-score')?.textContent).toBe('92')
    expect(card?.querySelector('.poster-title')?.textContent).toBe('葬送的芙莉莲')
    expect(card?.querySelector('.poster-meta')?.textContent).toContain('12 / 28 集')
    expect([...(card?.querySelectorAll('.poster-genre') ?? [])].map((g) => g.textContent)).toEqual([
      '奇幻', '冒险', '剧情',
    ])
    await unmount()
  })

  it('简介留在 DOM 里，只是视觉上默认收起（读屏拿得到）', async () => {
    const { container, unmount } = await mount(<ul>{FAKE_LISTS.watching.map(media => <MediaCard key={media.id} media={media} />)}</ul>)
    const over = container.querySelector('.poster-over-desc')
    expect(over).not.toBeNull()
    expect(over?.textContent).not.toBe('')
    // 不能用 hidden / display:none 藏 —— 那样读屏也读不到了
    expect(container.querySelector('.poster-over')?.getAttribute('hidden')).toBeNull()
    await unmount()
  })

  it('类型标签最多三个，多了会把卡片撑得高矮不一', async () => {
    const { container, unmount } = await mount(<ul>{FAKE_LISTS.watching.map(media => <MediaCard key={media.id} media={media} />)}</ul>)
    for (const card of container.querySelectorAll('.poster')) {
      expect(card.querySelectorAll('.poster-genre').length).toBeLessThanOrEqual(3)
    }
    await unmount()
  })

  it('评分为 0 或缺席时不出徽标，而不是显示一个 0 分', async () => {
    // 发现页作品卡（DiscoverCard）的同一条检查在 components/media/DiscoverInteractions.test.tsx
    const { container, unmount } = await mount(<ul><MediaCard media={{ ...FAKE_LISTS.watching[0]!, score: 0 }} /></ul>)
    expect(container.querySelector('.poster-score')).toBeNull()
    await unmount()
  })
})

describe('扩展 / Debrid', () => {
  it('扩展页：讲清规则与插件的区别，并挂上真实的源管理', async () => {
    // 这一页【不是】假数据：nagare 的规则系统是真的，只是之前埋在设置页里。
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        const data = {
          sources: [],
          rules: {
            remoteUrl: '',
            localDir: '',
            dir: '',
            loaded: 0,
            errors: [],
            lastLoadedAt: null,
            lastSyncAt: null,
          },
        }
        return new Response(JSON.stringify({ success: true, data }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }),
    )
    const { ExtensionsPage } = await import('./ExtensionsPage')
    const { container, unmount } = await mount(<ExtensionsPage />)

    expect(getByRole(container, 'heading', { level: 1 }).textContent).toBe('扩展')
    // 入口在设置里：页面顶部给一条回设置的路
    expect(getByRole(container, 'link', { name: '设置' }).getAttribute('href')).toBe('/settings#sources')
    // 从可执行插件那边过来的人会以为这里能装 JS 插件；这段对照必须在
    const intro = getByRole(container, 'region', { name: '规则，不是插件' }).textContent ?? ''
    expect(intro).toContain('声明式')
    expect(intro).toContain('没有代码执行')
    // 真实的源管理组件挂上了，而且直接挂在页面标题下（h2）
    expect(getByRole(container, 'region', { name: '磁力源规则' })).not.toBeNull()
    expect(getByRole(container, 'heading', { level: 2, name: '磁力源规则' })).not.toBeNull()
    // 不该有假数据横幅 —— 它用的是真接口
    expect(container.querySelector('.alert-warn')).toBeNull()
    await unmount()
    vi.unstubAllGlobals()
  })

  it('Debrid：说清没后端，且保存不会假装成功', async () => {
    const { DebridPage } = await import('./DebridPage')
    const { container, unmount } = await mount(<DebridPage />)

    const notice = container.querySelector('.alert-warn')?.textContent ?? ''
    expect(notice).toContain('后端没有对接')

    const input = container.querySelector<HTMLInputElement>('#debrid-key')
    // 密钥是凭证，必须遮蔽
    expect(input?.type).toBe('password')

    const btn = container.querySelector<HTMLButtonElement>('.btn')
    expect(btn?.disabled).toBe(true) // 空值不给点

    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        'value',
      )?.set
      setter?.call(input, 'k-123')
      input?.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => container.querySelector<HTMLButtonElement>('.btn')?.click())

    // 点了「保存」之后要如实说没保存，而不是给一个绿色的「已保存」
    expect(container.querySelector('.result--warn')?.textContent).toContain('没有保存')
    await unmount()
  })
})
