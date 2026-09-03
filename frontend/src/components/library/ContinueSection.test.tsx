// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { ContinueItem } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { ContinueSection } from './ContinueSection'

const ITEM: ContinueItem = {
  fileId: 'f1',
  title: '葬送的芙莉莲',
  episodeTitle: '第1话 冒险的终点',
  episode: 1,
  episodeCount: 28,
  cover: '/art/cap/f1',
  positionSec: 420,
  durationSec: 1400,
  updatedAt: 1_788_000_000_000,
}

describe('ContinueSection', () => {
  it('没有在看的条目时整块不渲染', async () => {
    const { container, unmount } = await mount(
      <ContinueSection items={[]} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    expect(container.querySelector('.cont')).toBeNull()
    await unmount()
  })

  it('渲染横幅、卡片与进度', async () => {
    const { container, unmount } = await mount(
      <ContinueSection items={[ITEM]} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    expect(container.querySelector('.cont-lead-title')?.textContent).toBe('葬送的芙莉莲')
    expect(container.querySelector('.cont-card-title')?.textContent).toBe('第1话 冒险的终点')
    expect(container.querySelector('.cont-card-meta')?.textContent).toContain('第 01 集 / 共 28 集')
    // 420 / 1400 = 30%
    expect(container.querySelector<HTMLElement>('.cont-bar-fill')?.style.width).toBe('30%')
    await unmount()
  })

  it('点卡片回调 fileId', async () => {
    const onPlay = vi.fn()
    const { container, unmount } = await mount(
      <ContinueSection items={[ITEM]} onPlay={onPlay} activeFileId={null} pendingFileId={null} />,
    )
    await act(async () => {
      container.querySelector<HTMLButtonElement>('.cont-card-hit')?.click()
    })
    expect(onPlay).toHaveBeenCalledWith('f1')
    await unmount()
  })

  it('请求在途时禁用，防连点', async () => {
    const onPlay = vi.fn()
    const { container, unmount } = await mount(
      <ContinueSection items={[ITEM]} onPlay={onPlay} activeFileId={null} pendingFileId="f1" />,
    )
    const btn = container.querySelector<HTMLButtonElement>('.cont-card-hit')
    expect(btn?.disabled).toBe(true)
    await act(async () => btn?.click())
    expect(onPlay).not.toHaveBeenCalled()
    await unmount()
  })

  it('封面加载失败时退回无图版式，不留破图', async () => {
    // 封面缺失/失败是常态：只有播放过的条目才匹配过、才有图。
    const { container, unmount } = await mount(
      <ContinueSection items={[ITEM]} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    const img = container.querySelector<HTMLImageElement>('.cont-thumb img')
    expect(img).not.toBeNull()
    await act(async () => {
      img?.dispatchEvent(new Event('error'))
    })
    expect(container.querySelector('.cont-thumb img')).toBeNull()
    expect(container.querySelector('.cont-thumb-blank')).not.toBeNull()
    await unmount()
  })

  it('没有封面字段时直接走无图版式', async () => {
    const noCover: ContinueItem = { ...ITEM, cover: undefined }
    const { container, unmount } = await mount(
      <ContinueSection items={[noCover]} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    expect(container.querySelector('.cont-thumb img')).toBeNull()
    expect(container.querySelector('.cont-backdrop')).toBeNull()
    expect(container.querySelector('.cont-thumb-blank')).not.toBeNull()
    await unmount()
  })
})
