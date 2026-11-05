// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SearchItem } from '../../lib/endpoints'
import { accessibleName, getAllByRole, getByRole } from '../../test/queries'
import { mount } from '../../test/harness'
import { MAX_RENDERED_RESULTS, ResultTable } from './ResultTable'
import { PLAY_BLOCKED_HINT, PLAY_ENGINE_DOWN_HINT, ResultRow } from './ResultRow'
import type { PlayControl } from './ResultRow'

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

const OTHER_MAGNET = 'magnet:?xt=urn:btih:89abcdef0123456789abcdef0123456789abcdef&dn=other'

/** 默认「没有磁力在播、引擎正常」；各用例按需覆盖 */
function makePlay(overrides: Partial<PlayControl> = {}): PlayControl {
  return { onPlay: vi.fn(), busy: null, engineDown: false, ...overrides }
}

/** 本行的播放按钮：名字是「看得见的字 + 这一行的标题」 */
function playButton(container: HTMLElement): HTMLButtonElement {
  return getByRole(container, 'button', { name: /^(播放|启动中 …|播放中) \[Sakurato\]/ }) as HTMLButtonElement
}

/** aria-describedby 指向的说明文字（禁用的原因） */
function description(element: HTMLElement): string {
  const ids = element.getAttribute('aria-describedby')?.split(/\s+/) ?? []
  return ids.map((id) => element.ownerDocument.getElementById(id)?.textContent ?? '').join(' ')
}

/** 给 jsdom 的 navigator 装一个可控的 clipboard */
function stubClipboard(writeText: (text: string) => Promise<void>): void {
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText },
    configurable: true,
  })
}

async function mountRow(item: SearchItem, showSeeders = false, play: PlayControl = makePlay()) {
  return mount(
    <table>
      <tbody>
        <ResultRow item={item} sourceName="源甲" showSeeders={showSeeders} play={play} />
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
    expect(getByRole(container, 'cell', { name: '源甲' })).not.toBeNull()
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
    const button = getByRole(container, 'button', { name: /^复制磁力 \[Sakurato\] Sousou no Frieren/ }) as HTMLButtonElement
    expect(button.textContent).toBe('复制磁力')

    await act(async () => {
      button.click()
    })
    expect(writeText).toHaveBeenCalledExactlyOnceWith(MAGNET)
    expect(button.textContent).toBe('已复制 ✓')
    await unmount()
  })

  it('整张表只有一个「已复制」播报区：复制哪一行就念哪一行，过一会儿清空', async () => {
    vi.useFakeTimers()
    try {
      stubClipboard(vi.fn<(text: string) => Promise<void>>().mockResolvedValue(undefined))
      const items = [makeItem(), makeItem({ title: '[Other] 另一条', magnet: OTHER_MAGNET })]
      const { container, unmount } = await mount(<ResultTable items={items} names={{ 'src-a': '源甲' }} play={makePlay()} />)
      expect(getAllByRole(container, 'status')).toHaveLength(1)
      await act(async () => getByRole(container, 'button', { name: /^复制磁力 \[Other\]/ }).click())
      expect(getByRole(container, 'status').textContent).toBe('已复制磁力：[Other] 另一条')
      await act(async () => getByRole(container, 'button', { name: /^复制磁力 \[Sakurato\]/ }).click())
      expect(getAllByRole(container, 'status').map((el) => el.textContent)).toEqual([`已复制磁力：${items[0]!.title}`])
      await act(async () => { vi.advanceTimersByTime(2000) })
      expect(getByRole(container, 'status').textContent).toBe('')
      await unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('剪贴板写入被拒时显示「复制失败」', async () => {
    stubClipboard(vi.fn().mockRejectedValue(new Error('NotAllowedError')))
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { container, unmount } = await mountRow(makeItem())
    const button = getByRole(container, 'button', { name: /^复制磁力/ }) as HTMLButtonElement
    await act(async () => {
      button.click()
    })
    expect(button.textContent).toBe('复制失败')
    expect(button.classList.contains('res-copy--failed')).toBe(true)
    expect(errorSpy).toHaveBeenCalled()
    errorSpy.mockRestore()
    await unmount()
  })

  it('「播放」可点，点击把本行交给磁力播放', async () => {
    const onPlay = vi.fn()
    const item = makeItem()
    const { container, unmount } = await mountRow(item, false, makePlay({ onPlay }))
    const play = playButton(container)
    expect(play.disabled).toBe(false)
    expect(play.textContent).toBe('播放')
    expect(container.querySelector('.res-play-wrap')?.getAttribute('title')).toBe(null)

    await act(async () => {
      play.click()
    })
    // 第二个参数是被点下的那个按钮：选集弹窗关闭后靠它把焦点还回原位
    expect(onPlay).toHaveBeenCalledExactlyOnceWith(item, play)
    await unmount()
  })

  it('本行正在起播：按钮显示「启动中 …」且禁用', async () => {
    const { container, unmount } = await mountRow(
      makeItem(),
      false,
      makePlay({ busy: { magnet: MAGNET, stage: 'pending' } }),
    )
    const play = playButton(container)
    // 请求在途：不可用但不是 disabled（选集弹窗关上时焦点要还到这里）
    expect(play.getAttribute('aria-disabled')).toBe('true')
    expect(play.disabled).toBe(false)
    expect(play.textContent).toBe('启动中 …')
    await unmount()
  })

  it('本行正在边下边播：按钮显示「播放中」并加高亮类', async () => {
    const { container, unmount } = await mountRow(
      makeItem(),
      false,
      makePlay({ busy: { magnet: MAGNET, stage: 'active' } }),
    )
    const play = playButton(container)
    expect(play.getAttribute('aria-disabled')).toBe('true')
    expect(play.textContent).toBe('播放中')
    expect(play.classList.contains('res-play--active')).toBe(true)
    await unmount()
  })

  it('别的行占着后端时本行禁用，并说明原因', async () => {
    const onPlay = vi.fn()
    const { container, unmount } = await mountRow(
      makeItem(),
      false,
      makePlay({ onPlay, busy: { magnet: OTHER_MAGNET, stage: 'pending' } }),
    )
    const play = playButton(container)
    expect(play.disabled).toBe(true)
    expect(play.textContent).toBe('播放')
    expect(container.querySelector('.res-play-wrap')?.getAttribute('title')).toBe(PLAY_BLOCKED_HINT)
    // 原因作为按钮的描述念出来；名字仍以看得见的「播放」开头
    expect(description(play)).toBe(PLAY_BLOCKED_HINT)
    expect(accessibleName(play)).toMatch(/^播放 /)

    // 禁用的按钮点了也不该触发播放（防「点了没反应」被误当成 bug）
    await act(async () => {
      play.click()
    })
    expect(onPlay).not.toHaveBeenCalled()
    await unmount()
  })

  it('磁力引擎不可用时整列禁用，并指向设置页', async () => {
    const { container, unmount } = await mountRow(makeItem(), false, makePlay({ engineDown: true }))
    const play = playButton(container)
    expect(play.disabled).toBe(true)
    expect(container.querySelector('.res-play-wrap')?.getAttribute('title')).toBe(
      PLAY_ENGINE_DOWN_HINT,
    )
    await unmount()
  })
})

describe('ResultTable', () => {
  it('超过上限只渲染前 N 行并提示', async () => {
    const items = Array.from({ length: MAX_RENDERED_RESULTS + 5 }, (_, index) =>
      makeItem({ title: `item ${index}` }),
    )
    const { container, unmount } = await mount(
      <ResultTable items={items} names={{ 'src-a': '源甲' }} play={makePlay()} />,
    )
    expect(container.querySelectorAll('tbody tr')).toHaveLength(MAX_RENDERED_RESULTS)
    expect(container.querySelector('.res-truncated')?.textContent).toContain(
      `只显示前 ${MAX_RENDERED_RESULTS} 条（共 ${items.length} 条）`,
    )
    await unmount()
  })

  it('没有任何一条带 seeders 时不出「做种」列', async () => {
    const { container, unmount } = await mount(
      <ResultTable items={[makeItem(), makeItem()]} names={{}} play={makePlay()} />,
    )
    expect(getAllByRole(container, 'columnheader').map(accessibleName)).not.toContain('做种')
    // 查不到名字时来源徽标回退显示 id
    expect(getAllByRole(container, 'cell', { name: 'src-a' })).toHaveLength(2)
    await unmount()
  })
})
