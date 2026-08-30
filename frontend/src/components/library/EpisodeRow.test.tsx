// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { LibraryItem } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { EpisodeRow } from './EpisodeRow'

const FILE_NAME = '[Sakurato] Sousou no Frieren [01][AVC-8bit 1080p AAC][CHS&CHT].mkv'

function makeItem(overrides: Partial<LibraryItem> = {}): LibraryItem {
  return {
    fileId: 'file-1',
    fileName: FILE_NAME,
    episode: 1,
    kind: 'main',
    resolution: '1080p',
    sizeBytes: 1610612736,
    progress: null,
    ...overrides,
  }
}

async function mountRow(item: LibraryItem, onPlay: (fileId: string) => void = () => {}) {
  // 行组件是 <li>，按语义包一层 <ul> 挂载
  return mount(
    <ul>
      <EpisodeRow item={item} onPlay={onPlay} />
    </ul>,
  )
}

describe('EpisodeRow', () => {
  it('有集号：补零显示集号，不渲染文件名（全名挂在行 title 上）', async () => {
    const { container, unmount } = await mountRow(makeItem({ episode: 3 }))
    expect(container.querySelector('.ep-num')?.textContent).toBe('03')
    expect(container.querySelector('.ep-name')).toBeNull()
    expect(container.querySelector('.ep-row')?.getAttribute('title')).toBe(FILE_NAME)
    await unmount()
  })

  it('无集号：集号位显示占位符，主列回退为文件名', async () => {
    const { container, unmount } = await mountRow(makeItem({ episode: null }))
    expect(container.querySelector('.ep-num')?.textContent).toBe('—')
    expect(container.querySelector('.ep-name')?.textContent).toBe(FILE_NAME)
    await unmount()
  })

  it('kind=main 不显示徽标', async () => {
    const { container, unmount } = await mountRow(makeItem({ kind: 'main' }))
    expect(container.querySelector('.badge')).toBeNull()
    await unmount()
  })

  it.each<[string, string]>([
    ['sp', 'SP'],
    ['ova', 'OVA'],
    ['ncop', 'NCOP'],
  ])('kind=%s → 徽标 %s', async (kind, expected) => {
    const { container, unmount } = await mountRow(makeItem({ kind }))
    expect(container.querySelector('.badge')?.textContent).toBe(expected)
    await unmount()
  })

  it('分辨率为 null 时该列留空', async () => {
    const { container, unmount } = await mountRow(makeItem({ resolution: null }))
    expect(container.querySelector('.ep-res')?.textContent).toBe('')
    await unmount()
  })

  it('progress=null：不渲染 progressbar', async () => {
    const { container, unmount } = await mountRow(makeItem({ progress: null }))
    expect(container.querySelector('[role="progressbar"]')).toBeNull()
    expect(container.querySelector('.ep-done')).toBeNull()
    await unmount()
  })

  it('看到一半：progressbar 带 aria-valuenow，填充宽度与百分比一致', async () => {
    const { container, unmount } = await mountRow(
      makeItem({ progress: { positionSec: 710, durationSec: 1420, completed: false } }),
    )
    const bar = container.querySelector('[role="progressbar"]')
    expect(bar).not.toBeNull()
    expect(bar?.getAttribute('aria-valuenow')).toBe('50')
    expect(bar?.getAttribute('aria-valuemin')).toBe('0')
    expect(bar?.getAttribute('aria-valuemax')).toBe('100')
    const fill = container.querySelector('.ep-pbar-fill') as HTMLElement
    expect(fill.style.width).toBe('50%')
    expect(container.querySelector('.ep-pct')?.textContent).toBe('50%')
    await unmount()
  })

  it('看完：显示 ✓，不再画进度条', async () => {
    const { container, unmount } = await mountRow(
      makeItem({ progress: { positionSec: 1420, durationSec: 1420, completed: true } }),
    )
    expect(container.querySelector('.ep-done')?.textContent).toBe('✓')
    expect(container.querySelector('[role="progressbar"]')).toBeNull()
    await unmount()
  })

  it('点击 ▶ 回调 fileId；按钮带可读的 aria-label', async () => {
    const onPlay = vi.fn()
    const { container, unmount } = await mountRow(makeItem({ episode: 1 }), onPlay)
    const button = container.querySelector('button.ep-play') as HTMLButtonElement
    expect(button.getAttribute('aria-label')).toBe('播放 第01集')
    await act(async () => {
      button.click()
    })
    expect(onPlay).toHaveBeenCalledExactlyOnceWith('file-1')
    await unmount()
  })

  it('无集号时 aria-label 回退为文件名', async () => {
    const { container, unmount } = await mountRow(makeItem({ episode: null }))
    expect(container.querySelector('button.ep-play')?.getAttribute('aria-label')).toBe(
      `播放 ${FILE_NAME}`,
    )
    await unmount()
  })

  it('isPending 时播放按钮禁用', async () => {
    const { container, unmount } = await mount(
      <ul>
        <EpisodeRow item={makeItem()} onPlay={() => {}} isPending />
      </ul>,
    )
    expect((container.querySelector('button.ep-play') as HTMLButtonElement).disabled).toBe(true)
    await unmount()
  })
})
