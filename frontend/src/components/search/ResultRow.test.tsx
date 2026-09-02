// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SearchItem } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { MAX_RENDERED_RESULTS, ResultTable } from './ResultTable'
import { PLAY_UNAVAILABLE_HINT, ResultRow } from './ResultRow'

const MAGNET = 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=frieren'

function makeItem(overrides: Partial<SearchItem> = {}): SearchItem {
  return {
    title: '[Sakurato] Sousou no Frieren [01][AVC-8bit 1080p AAC][CHS&CHT]',
    magnet: MAGNET,
    size: '1.5 GB',
    fansub: '桜都字幕组',
    date: '2026-08-30',
    source: 'src-a',
    ...overrides,
  }
}

/** 给 jsdom 的 navigator 装一个可控的 clipboard */
function stubClipboard(writeText: (text: string) => Promise<void>): void {
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText },
    configurable: true,
  })
}

async function mountRow(item: SearchItem, showSeeders = false) {
  return mount(
    <table>
      <tbody>
        <ResultRow item={item} sourceName="源甲" showSeeders={showSeeders} />
      </tbody>
    </table>,
  )
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'clipboard')
})

describe('ResultRow', () => {
  it('渲染标题 / 体积 / 字幕组 / 日期（原样）/ 来源徽标', async () => {
    const { container, unmount } = await mountRow(makeItem())
    expect(container.querySelector('.res-title-text')?.textContent).toContain('Sousou no Frieren')
    expect(container.querySelector('.res-size')?.textContent).toBe('1.5 GB')
    expect(container.querySelector('.res-fansub')?.textContent).toBe('桜都字幕组')
    expect(container.querySelector('.res-date')?.textContent).toBe('2026-08-30')
    expect(container.querySelector('.res-source .badge')?.textContent).toBe('源甲')
    expect(container.querySelector('.res-seeders')).toBeNull()
    await unmount()
  })

  it('fansub / date 为 null 时留空；showSeeders 时渲染做种数列', async () => {
    const { container, unmount } = await mountRow(
      makeItem({ fansub: null, date: null, seeders: 42 }),
      true,
    )
    expect(container.querySelector('.res-fansub')?.textContent).toBe('')
    expect(container.querySelector('.res-date')?.textContent).toBe('')
    expect(container.querySelector('.res-seeders')?.textContent).toBe('42')
    await unmount()
  })

  it('「复制磁力」把 magnet 写进剪贴板，并给出短提示', async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>().mockResolvedValue(undefined)
    stubClipboard(writeText)
    const { container, unmount } = await mountRow(makeItem())
    const button = container.querySelector('button.res-copy') as HTMLButtonElement
    expect(button.textContent).toBe('复制磁力')

    await act(async () => {
      button.click()
    })
    expect(writeText).toHaveBeenCalledExactlyOnceWith(MAGNET)
    expect(button.textContent).toBe('已复制 ✓')
    expect(container.querySelector('[role="status"]')?.textContent).toBe('已复制 ✓')
    await unmount()
  })

  it('剪贴板写入被拒时显示「复制失败」', async () => {
    stubClipboard(vi.fn().mockRejectedValue(new Error('NotAllowedError')))
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { container, unmount } = await mountRow(makeItem())
    const button = container.querySelector('button.res-copy') as HTMLButtonElement
    await act(async () => {
      button.click()
    })
    expect(button.textContent).toBe('复制失败')
    expect(button.classList.contains('res-copy--failed')).toBe(true)
    expect(errorSpy).toHaveBeenCalled()
    errorSpy.mockRestore()
    await unmount()
  })

  it('「播放」禁用并提示边下边播在 M3', async () => {
    const { container, unmount } = await mountRow(makeItem())
    const play = container.querySelector('.res-play-wrap button') as HTMLButtonElement
    expect(play.disabled).toBe(true)
    expect(container.querySelector('.res-play-wrap')?.getAttribute('title')).toBe(PLAY_UNAVAILABLE_HINT)
    expect(play.getAttribute('aria-label')).toContain('下一里程碑')
    await unmount()
  })
})

describe('ResultTable', () => {
  it('超过上限只渲染前 N 行并提示', async () => {
    const items = Array.from({ length: MAX_RENDERED_RESULTS + 5 }, (_, index) =>
      makeItem({ title: `item ${index}` }),
    )
    const { container, unmount } = await mount(<ResultTable items={items} names={{ 'src-a': '源甲' }} />)
    expect(container.querySelectorAll('tbody tr')).toHaveLength(MAX_RENDERED_RESULTS)
    expect(container.querySelector('.res-truncated')?.textContent).toContain(
      `只显示前 ${MAX_RENDERED_RESULTS} 条（共 ${items.length} 条）`,
    )
    await unmount()
  })

  it('没有任何一条带 seeders 时不出「做种」列', async () => {
    const { container, unmount } = await mount(
      <ResultTable items={[makeItem(), makeItem()]} names={{}} />,
    )
    expect(container.querySelector('th.res-seeders')).toBeNull()
    // 查不到名字时来源徽标回退显示 id
    expect(container.querySelector('.res-source .badge')?.textContent).toBe('src-a')
    await unmount()
  })
})
