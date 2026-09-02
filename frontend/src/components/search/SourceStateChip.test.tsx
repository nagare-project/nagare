// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { SourceOutcome } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { SourceStateChip } from './SourceStateChip'

function outcome(overrides: Partial<SourceOutcome> = {}): SourceOutcome {
  return {
    source: 'src-a',
    state: 'ok',
    count: 3,
    rawCount: 3,
    dropped: 0,
    latencyMs: 80,
    ...overrides,
  }
}

describe('SourceStateChip', () => {
  it.each<[SourceOutcome['state'], string]>([
    ['ok', '3 条'],
    ['zero', '无结果'],
    ['dead', '源异常'],
    ['failed', '连接失败'],
    ['disabled', '已禁用'],
  ])('state=%s → 文案「%s」+ 对应样式类与 data-state', async (state, expected) => {
    const { container, unmount } = await mount(
      <SourceStateChip name="源甲" outcome={outcome({ state })} />,
    )
    const chip = container.querySelector('.src-chip')
    expect(chip).not.toBeNull()
    expect(chip?.classList.contains(`src-chip--${state}`)).toBe(true)
    expect(chip?.getAttribute('data-state')).toBe(state)
    expect(chip?.querySelector('.src-chip-name')?.textContent).toBe('源甲')
    expect(chip?.querySelector('.src-chip-state')?.textContent).toBe(expected)
    await unmount()
  })

  it('reason / detail 挂在 title 上，aria-label 说明名称与状态', async () => {
    const { container, unmount } = await mount(
      <SourceStateChip
        name="源甲"
        outcome={outcome({
          state: 'dead',
          reason: '规则解析不出任何条目',
          detail: 'selector matched 0 nodes',
          latencyMs: -1,
        })}
      />,
    )
    const chip = container.querySelector('.src-chip')
    expect(chip?.getAttribute('title')).toBe('规则解析不出任何条目\nselector matched 0 nodes')
    expect(chip?.getAttribute('aria-label')).toBe('源甲：源异常')
    await unmount()
  })

  it('不传 onToggle：只读徽标，role=status，不是按钮', async () => {
    const { container, unmount } = await mount(
      <SourceStateChip name="源甲" outcome={outcome({ state: 'zero' })} />,
    )
    const chip = container.querySelector('.src-chip')
    expect(chip?.tagName).toBe('SPAN')
    expect(chip?.getAttribute('role')).toBe('status')
    await unmount()
  })

  it('传 onToggle：变成按钮，aria-pressed 反映启用态，点击回调取反', async () => {
    const onToggle = vi.fn()
    const { container, unmount } = await mount(
      <SourceStateChip name="源甲" outcome={outcome({ state: 'ok' })} onToggle={onToggle} />,
    )
    const button = container.querySelector('button.src-chip') as HTMLButtonElement
    expect(button.getAttribute('aria-pressed')).toBe('true')
    expect(button.getAttribute('aria-label')).toBe('源甲：正常 · 3 条，点击禁用')
    await act(async () => {
      button.click()
    })
    expect(onToggle).toHaveBeenCalledExactlyOnceWith(false)
    await unmount()
  })

  it('disabled 态的按钮：aria-pressed=false，点击回调 true（启用）', async () => {
    const onToggle = vi.fn()
    const { container, unmount } = await mount(
      <SourceStateChip name="源甲" outcome={outcome({ state: 'disabled' })} onToggle={onToggle} />,
    )
    const button = container.querySelector('button.src-chip') as HTMLButtonElement
    expect(button.getAttribute('aria-pressed')).toBe('false')
    expect(button.getAttribute('aria-label')).toBe('源甲：已禁用，点击启用')
    await act(async () => {
      button.click()
    })
    expect(onToggle).toHaveBeenCalledExactlyOnceWith(true)
    await unmount()
  })

  it('busy 时按钮禁用', async () => {
    const { container, unmount } = await mount(
      <SourceStateChip name="源甲" outcome={outcome()} onToggle={() => {}} busy />,
    )
    expect((container.querySelector('button.src-chip') as HTMLButtonElement).disabled).toBe(true)
    await unmount()
  })
})
