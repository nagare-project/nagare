// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { mount } from '../test/harness'
import { DiscoverPage } from './DiscoverPage'
import { ListsPage } from './ListsPage'
import { SchedulePage } from './SchedulePage'
import { FAKE_LISTS } from '../lib/fixtures/library'

/**
 * 三个吃假数据的页面（我的列表 / 发现 / 放送表）。
 *
 * 这里守的重点不是像素，是【不能把假数据伪装成真的】：每一页都必须
 * 挂着那条假数据横幅。哪天有人接了真接口忘了摘横幅，或者反过来
 * 把横幅删了却还在用 fixture，这几条会红。
 */

describe('假数据页面', () => {
  it('我的列表：默认「在看」档，标签可切换', async () => {
    const { container, unmount } = await mount(<ListsPage />)
    expect(container.querySelector('.page-title')?.textContent).toBe('我的列表')

    const tabs = [...container.querySelectorAll('.tab')]
    expect(tabs.map((t) => t.textContent?.replace(/\d+$/, ''))).toEqual([
      '在看', '想看', '看完', '搁置', '弃番',
    ])
    expect(container.querySelector('.tab--on')?.textContent).toContain('在看')
    expect(container.querySelectorAll('.poster')).toHaveLength(FAKE_LISTS.watching.length)

    // 切到「弃番」：这一档是空的，要出空态而不是空白
    const dropped = tabs[4]!
    await act(async () => (dropped as HTMLButtonElement).click())
    expect(container.querySelectorAll('.poster')).toHaveLength(0)
    expect(container.textContent).toContain('这一档还没有作品')
    await unmount()
  })

  it('发现：板块顺序对齐 seanime，每块都有卡片', async () => {
    const { container, unmount } = await mount(<DiscoverPage />)
    const rows = [...container.querySelectorAll('.row')]
    // 顺序照 seanime 的 anime 标签页；改这里之前先确认那边也改了
    expect(rows.map((r) => r.querySelector('.row-title')?.textContent)).toEqual([
      '本季热门', '最近更新', '本季新番', '上季作品', '错过的续作', '即将播出', '剧场版',
    ])
    for (const row of rows) {
      expect(row.querySelectorAll('.poster').length).toBeGreaterThan(0)
    }
    await unmount()
  })

  it('发现：hero 轮播可点圆点切换，且不含预告片 iframe', async () => {
    const { container, unmount } = await mount(<DiscoverPage />)
    const hero = container.querySelector('.hero')
    expect(hero).not.toBeNull()
    expect(hero?.querySelector('.hero-title')?.textContent).not.toBe('')

    // CSP 没开 frame-src：seanime 那里嵌 YouTube 预告片，我们刻意不做。
    // 哪天有人加了 iframe，这条会红。
    expect(container.querySelector('iframe')).toBeNull()

    const dots = [...container.querySelectorAll<HTMLButtonElement>('.hero-dot')]
    expect(dots.length).toBeGreaterThan(1)
    const first = hero?.querySelector('.hero-title')?.textContent
    await act(async () => dots[1]!.click())
    expect(container.querySelector('.hero-title')?.textContent).not.toBe(first)
    await unmount()
  })

  it('发现：切到「放送表」标签复用同一个周历，不重复页头', async () => {
    const { container, unmount } = await mount(<DiscoverPage />)
    expect(container.querySelector('.week')).toBeNull()
    const scheduleTab = [...container.querySelectorAll<HTMLButtonElement>('.tab')].find(
      (t) => t.textContent === '放送表',
    )
    await act(async () => scheduleTab?.click())
    expect(container.querySelectorAll('.day')).toHaveLength(7)
    // 内嵌时不该冒出第二个 <h1>
    expect(container.querySelectorAll('.page-title')).toHaveLength(0)
    await unmount()
  })

  it('放送表：七天都在，今天有标记', async () => {
    const { container, unmount } = await mount(<SchedulePage />)
    expect(container.querySelectorAll('.day')).toHaveLength(7)
    // 「今天」有且只有一个 —— 多于一个说明周几的换算错了
    expect(container.querySelectorAll('.day--today')).toHaveLength(1)
    expect(container.querySelector('.day--today')?.textContent).toContain('今天')
    // 至少排了几场
    expect(container.querySelectorAll('.airing').length).toBeGreaterThan(3)
    await unmount()
  })

  it('每一页都挂着假数据横幅，不许把假数据伪装成真的', async () => {
    for (const [name, Page] of [
      ['我的列表', ListsPage],
      ['发现', DiscoverPage],
      ['放送表', SchedulePage],
    ] as const) {
      const { container, unmount } = await mount(<Page />)
      const notice = container.querySelector('.alert-warn')
      expect(notice, `${name} 缺少假数据横幅`).not.toBeNull()
      expect(notice?.textContent).toContain('假数据')
      expect(notice?.textContent).toContain('todos.md')
      await unmount()
    }
  })
})

describe('扫描记录 / 自动下载', () => {
  it('扫描记录：列出每次扫描，解析失败的文件必须可见', async () => {
    const { ScanSummariesPage } = await import('./ScanSummariesPage')
    const { container, unmount } = await mount(<ScanSummariesPage />)
    expect(container.querySelectorAll('.scan').length).toBeGreaterThan(1)
    // 解析不出集号的文件会被静默跳过，用户唯一能察觉的方式就是这一段。
    // 它消失了 = 又变回静默失败，所以钉住。
    const unresolved = container.querySelector('.scan-unresolved')
    expect(unresolved).not.toBeNull()
    expect(unresolved?.textContent).toContain('解析不出集号')
    await unmount()
  })

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
    const { container, unmount } = await mount(<ListsPage />)
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
    const { container, unmount } = await mount(<ListsPage />)
    const over = container.querySelector('.poster-over-desc')
    expect(over).not.toBeNull()
    expect(over?.textContent).not.toBe('')
    // 不能用 hidden / display:none 藏 —— 那样读屏也读不到了
    expect(container.querySelector('.poster-over')?.getAttribute('hidden')).toBeNull()
    await unmount()
  })

  it('类型标签最多三个，多了会把卡片撑得高矮不一', async () => {
    const { container, unmount } = await mount(<ListsPage />)
    for (const card of container.querySelectorAll('.poster')) {
      expect(card.querySelectorAll('.poster-genre').length).toBeLessThanOrEqual(3)
    }
    await unmount()
  })

  it('评分为 0 或缺席时不出徽标，而不是显示一个 0 分', async () => {
    const { DiscoverPage } = await import('./DiscoverPage')
    const { container, unmount } = await mount(<DiscoverPage />)
    // 「即将播出」里那部是刻意造的半空条目：评分 0、集数未知
    const scores = [...container.querySelectorAll('.poster-score')].map((e) => e.textContent)
    expect(scores).not.toContain('0')
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

    expect(container.querySelector('.page-title')?.textContent).toBe('扩展')
    // 从 seanime 过来的人会以为这里能装 JS 插件；这段对照必须在
    const intro = container.querySelector('.ext-intro')?.textContent ?? ''
    expect(intro).toContain('声明式')
    expect(intro).toContain('没有代码执行')
    // 真实的源管理组件挂上了
    expect(container.querySelector('[aria-labelledby="sources-heading"]')).not.toBeNull()
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
