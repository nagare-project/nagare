// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it } from 'vitest'
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

  it('发现：三个板块各有卡片', async () => {
    const { container, unmount } = await mount(<DiscoverPage />)
    const rows = [...container.querySelectorAll('.row')]
    expect(rows.map((r) => r.querySelector('.row-title')?.textContent)).toEqual([
      '本季热门', '高人气', '即将播出',
    ])
    for (const row of rows) {
      expect(row.querySelectorAll('.poster').length).toBeGreaterThan(0)
    }
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
