// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { ContinueItem, PosterTones } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { getAllByRole, getByRole, getByText, queryByRole } from '../../test/queries'
import { ContinueSection, continueMeta } from './ContinueSection'

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

const TONES: PosterTones = { hue: 30, tone: '#e0b49a', quiet: '#c29a82', mid: '#8a6450', low: '#2b1d15', onTone: '#120a06' }

/** 卡片就是一个按钮，名字以「继续播放 作品名」开头 */
const card = (container: HTMLElement, title = ITEM.title) => getByRole(container, 'button', { name: new RegExp(`^继续播放 ${title}`) })

describe('ContinueSection', () => {
  it('没有在看的条目时整块不渲染', async () => {
    const { container, unmount } = await mount(
      <ContinueSection items={[]} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    expect(queryByRole(container, 'region', { name: '继续观看' })).toBeNull()
    expect(container.textContent).toBe('')
    await unmount()
  })

  it('渲染区块标题、卡片与进度：名字说出作品、第几集、还剩多久', async () => {
    const { container, unmount } = await mount(
      <ContinueSection items={[ITEM]} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    const section = getByRole(container, 'region', { name: '继续观看' })
    expect(getByRole(section, 'heading', { level: 2 }).textContent).toBe('继续观看')
    // 1400 - 420 = 980 秒 ≈ 16 分钟
    expect(getAllByRole(section, 'button').map((b) => b.textContent)).toEqual(['继续播放 葬送的芙莉莲 第 01 集 · 还剩 16 分钟 第1话 冒险的终点'])
    // 进度条只是视觉（读屏念的是「还剩 16 分钟」）：420 / 1400 = 30%
    const widths = [...card(container).querySelectorAll<HTMLElement>('[aria-hidden="true"] [style]')].map((el) => el.style.width).filter(Boolean)
    expect(widths).toEqual(['30%'])
    await unmount()
  })

  it('最多三条（DESIGN.md「媒体库 ①」）', async () => {
    const items = [1, 2, 3, 4].map((n) => ({ ...ITEM, fileId: `f${n}`, title: `作品 ${n}` }))
    const { container, unmount } = await mount(
      <ContinueSection items={items} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    expect(getAllByRole(container, 'button').map((b) => b.textContent?.startsWith('继续播放 作品 '))).toEqual([true, true, true])
    expect(container.textContent).not.toContain('作品 4')
    await unmount()
  })

  it('时长未知时只说第几集', () => {
    expect(continueMeta({ ...ITEM, durationSec: 0 })).toBe('第 01 集')
    expect(continueMeta({ ...ITEM, episode: null })).toBe('还剩 16 分钟')
  })

  it('点卡片回调 fileId', async () => {
    const onPlay = vi.fn()
    const { container, unmount } = await mount(
      <ContinueSection items={[ITEM]} onPlay={onPlay} activeFileId={null} pendingFileId={null} />,
    )
    await act(async () => card(container).click())
    expect(onPlay).toHaveBeenCalledWith('f1')
    await unmount()
  })

  it('请求在途时不可用（aria-disabled，焦点留在卡片上），防连点', async () => {
    const onPlay = vi.fn()
    const { container, unmount } = await mount(
      <ContinueSection items={[ITEM]} onPlay={onPlay} activeFileId={null} pendingFileId="f1" />,
    )
    const btn = card(container) as HTMLButtonElement
    expect(btn.getAttribute('aria-disabled')).toBe('true')
    expect(btn.disabled).toBe(false)
    await act(async () => btn.click())
    expect(onPlay).not.toHaveBeenCalled()
    await unmount()
  })

  it('正在播放的那条：名字里说出「正在播放」', async () => {
    const { container, unmount } = await mount(
      <ContinueSection items={[ITEM]} onPlay={vi.fn()} activeFileId="f1" pendingFileId={null} />,
    )
    expect(card(container).textContent).toContain('（正在播放）')
    await unmount()
  })

  it('海报色写在卡片根元素自己身上（悬停描边用）；没有色阶时不写，落回中性值', async () => {
    const toned = await mount(
      <ContinueSection items={[{ ...ITEM, posterTones: TONES }]} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    expect(card(toned.container).closest('li')!.style.getPropertyValue('--poster-tone-mid')).toBe('#8a6450')
    await toned.unmount()
    const plain = await mount(
      <ContinueSection items={[{ ...ITEM, posterTones: null }]} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    expect(card(plain.container).closest('li')!.getAttribute('style')).toBeNull()
    await plain.unmount()
  })

  it('封面加载失败时退回无图版式，不留破图', async () => {
    // 封面缺失/失败是常态：只有播放过的条目才匹配过、才有图。
    const { container, unmount } = await mount(
      <ContinueSection items={[ITEM]} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    const img = card(container).querySelector('img')
    expect(img?.getAttribute('src')).toBe('/art/cap/f1')
    await act(async () => {
      img?.dispatchEvent(new Event('error'))
    })
    expect(card(container).querySelector('img')).toBeNull()
    // 无图版式：一个装饰用的「流」字（读屏不念）
    expect(getByText(card(container), '流', { hidden: true }).getAttribute('aria-hidden')).toBe('true')
    await unmount()
  })

  it('没有封面字段时直接走无图版式', async () => {
    const noCover: ContinueItem = { ...ITEM, cover: undefined }
    const { container, unmount } = await mount(
      <ContinueSection items={[noCover]} onPlay={vi.fn()} activeFileId={null} pendingFileId={null} />,
    )
    expect(container.querySelector('img')).toBeNull()
    expect(getByText(card(container), '流', { hidden: true })).toBeDefined()
    await unmount()
  })
})
