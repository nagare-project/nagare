// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TorrentFile } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { EpisodePicker } from './EpisodePicker'

const FILES: TorrentFile[] = [
  {
    index: 0,
    name: '[Sakurato] Sousou no Frieren [01][1080p].mkv',
    path: 'Frieren/[Sakurato] Sousou no Frieren [01][1080p].mkv',
    sizeBytes: 1_610_612_736, // 1.5 GB
    episode: 1,
  },
  {
    index: 3,
    name: '[Sakurato] Sousou no Frieren [02][1080p].mkv',
    path: 'Frieren/[Sakurato] Sousou no Frieren [02][1080p].mkv',
    sizeBytes: 1_073_741_824, // 1 GB
    episode: 2,
  },
  {
    index: 7,
    name: 'Bonus - Interview.mkv',
    path: 'Frieren/Bonus - Interview.mkv',
    sizeBytes: 104_857_600, // 100 MB
    episode: null,
  },
]

const mounted: Array<() => Promise<void>> = []

async function mountPicker(overrides: Partial<Parameters<typeof EpisodePicker>[0]> = {}) {
  const props = {
    title: '[Sakurato] Sousou no Frieren [Season 1][1080p]',
    files: FILES,
    onSelect: vi.fn<(fileIndex: number) => void>(),
    onCancel: vi.fn<() => void>(),
    ...overrides,
  }
  const result = await mount(<EpisodePicker {...props} />)
  mounted.push(result.unmount)
  return { ...result, props }
}

function items(container: HTMLElement): HTMLButtonElement[] {
  return Array.from(container.querySelectorAll<HTMLButtonElement>('.ep-item'))
}

/** 在弹窗上派发一次键盘事件（React 的 onKeyDown 走冒泡的 keydown） */
async function pressKey(container: HTMLElement, key: string, shiftKey = false): Promise<void> {
  const dialog = container.querySelector('[role="dialog"]')
  if (dialog === null) throw new Error('找不到弹窗')
  await act(async () => {
    dialog.dispatchEvent(new KeyboardEvent('keydown', { key, shiftKey, bubbles: true }))
  })
}

afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.restoreAllMocks()
})

describe('EpisodePicker', () => {
  it('按 files 渲染出列表：集号补零 / 无集号留空 / 体积人类可读', async () => {
    const { container } = await mountPicker()
    const rows = items(container)
    expect(rows).toHaveLength(3)

    expect(rows[0]?.querySelector('.ep-ep')?.textContent).toBe('01')
    expect(rows[0]?.querySelector('.ep-name')?.textContent).toContain('Frieren [01]')
    expect(rows[0]?.querySelector('.ep-size')?.textContent).toBe('1.5 GB')

    expect(rows[1]?.querySelector('.ep-ep')?.textContent).toBe('02')
    expect(rows[1]?.querySelector('.ep-size')?.textContent).toBe('1 GB')

    // 解析不出集号的花絮：集号列留空，不硬编一个假集数
    expect(rows[2]?.querySelector('.ep-ep')?.textContent).toBe('')
    expect(rows[2]?.querySelector('.ep-size')?.textContent).toBe('100 MB')
  })

  it('点击某项回调带该文件的 index（不是数组下标）', async () => {
    const { container, props } = await mountPicker()
    await act(async () => {
      items(container)[1]?.click()
    })
    expect(props.onSelect).toHaveBeenCalledExactlyOnceWith(3)
    expect(props.onCancel).not.toHaveBeenCalled()
  })

  it('Esc 关闭（走 onCancel，由调用方去 stop 释放种子）', async () => {
    const { container, props } = await mountPicker()
    await pressKey(container, 'Escape')
    expect(props.onCancel).toHaveBeenCalledTimes(1)
    expect(props.onSelect).not.toHaveBeenCalled()
  })

  it('「取消」按钮同样走 onCancel，并说明会放弃这条磁力', async () => {
    const { container, props } = await mountPicker()
    const cancel = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === '取消',
    )
    await act(async () => {
      cancel?.click()
    })
    expect(props.onCancel).toHaveBeenCalledTimes(1)
    expect(container.querySelector('.ep-hint')?.textContent).toContain('放弃这条磁力')
  })

  it('可达性：role=dialog + aria-modal + 可访问名，焦点落在第一项', async () => {
    const { container } = await mountPicker()
    const dialog = container.querySelector('[role="dialog"]')
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    const labelledBy = dialog?.getAttribute('aria-labelledby')
    expect(labelledBy).toBe('episode-picker-heading')
    expect(container.querySelector(`#${labelledBy}`)?.textContent).toBe('选择要播放的剧集')
    expect(document.activeElement).toBe(items(container)[0])
  })

  it('Tab 在弹窗内循环：最后一个再 Tab 回到第一个，反向亦然', async () => {
    const { container } = await mountPicker()
    const focusable = Array.from(container.querySelectorAll<HTMLElement>('button:not(:disabled)'))
    const first = focusable[0]
    const last = focusable[focusable.length - 1]

    last?.focus()
    await pressKey(container, 'Tab')
    expect(document.activeElement).toBe(first)

    await pressKey(container, 'Tab', true)
    expect(document.activeElement).toBe(last)
  })

  it('files 为空时给出明确提示，而不是一个空弹窗', async () => {
    const { container } = await mountPicker({ files: [] })
    expect(items(container)).toHaveLength(0)
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('没有可播放的视频文件')
  })
})

describe('EpisodePicker · 焦点交还', () => {
  // 回归：弹窗关闭后必须把焦点交给调用方指定的元素。
  // 这里【不能】自己读 document.activeElement 记「谁打开了我」—— 触发它的播放按钮
  // 在点下的同一次渲染里就自我禁用了，等弹窗挂载时 activeElement 已经是 <body>。
  it('卸载时调用 restoreFocus（而不是自己猜谁打开了它）', async () => {
    const restoreFocus = vi.fn<() => void>()
    // 模拟真实时序：打开弹窗时焦点已经不在触发按钮上了
    document.body.focus()
    const { unmount } = await mountPicker({ restoreFocus })

    expect(restoreFocus).not.toHaveBeenCalled()
    await act(async () => {
      await unmount()
      // 交还推迟一帧：卸载清理跑在提交新 DOM 之前，那时按钮的禁用状态还是旧的
      await new Promise((resolve) => requestAnimationFrame(() => resolve(null)))
    })
    expect(restoreFocus).toHaveBeenCalledOnce()
  })

  it('没传 restoreFocus 时不报错（这个 prop 是可选的）', async () => {
    const { unmount } = await mountPicker()
    await act(async () => {
      await unmount()
      await new Promise((resolve) => requestAnimationFrame(() => resolve(null)))
    })
  })
})
