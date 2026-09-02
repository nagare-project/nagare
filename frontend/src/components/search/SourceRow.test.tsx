// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SourceInfo } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { SourceRow } from './SourceRow'

function makeSource(overrides: Partial<SourceInfo> = {}): SourceInfo {
  return {
    id: 'src-a',
    name: '源甲',
    homepage: 'https://example.test',
    enabled: true,
    capabilities: { seeders: false, priority: 0 },
    hasSelfTest: true,
    ...overrides,
  }
}

async function mountRow(source: SourceInfo, props: Partial<Parameters<typeof SourceRow>[0]> = {}) {
  return mount(
    <ul>
      <SourceRow
        source={source}
        check={undefined}
        toggling={false}
        onToggle={() => {}}
        onSelfCheck={() => {}}
        {...props}
      />
    </ul>,
  )
}

const mounted: Array<() => Promise<void>> = []
afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
})

describe('SourceRow 主页链接守卫（规则文件是用户提供的）', () => {
  it('http(s) 主页渲染成新标签页链接', async () => {
    const m = await mountRow(makeSource({ homepage: 'https://example.test/x' }))
    mounted.push(m.unmount)
    const a = m.container.querySelector('a.source-home')
    expect(a).not.toBeNull()
    expect(a?.getAttribute('href')).toBe('https://example.test/x')
    expect(a?.getAttribute('rel')).toContain('noopener')
    expect(a?.getAttribute('target')).toBe('_blank')
  })

  it('javascript: 之类的主页绝不变成可点的 <a>', async () => {
    const m = await mountRow(makeSource({ homepage: 'javascript:alert(1)' }))
    mounted.push(m.unmount)
    expect(m.container.querySelector('a')).toBeNull()
    expect(m.container.querySelector('.source-home-text')?.textContent).toBe('javascript:alert(1)')
  })

  it('空主页什么都不渲染', async () => {
    const m = await mountRow(makeSource({ homepage: '' }))
    mounted.push(m.unmount)
    expect(m.container.querySelector('a')).toBeNull()
    expect(m.container.querySelector('.source-home-text')).toBeNull()
  })
})

describe('SourceRow 开关与自检', () => {
  it('点开关回调取反后的启用状态', async () => {
    const onToggle = vi.fn()
    const m = await mountRow(makeSource({ enabled: true }), { onToggle })
    mounted.push(m.unmount)
    const sw = m.container.querySelector<HTMLButtonElement>('[role="switch"]')
    expect(sw?.getAttribute('aria-checked')).toBe('true')
    await act(async () => {
      sw?.click()
    })
    expect(onToggle).toHaveBeenCalledWith(false)
  })

  it('规则没有自检关键词时自检按钮禁用', async () => {
    const onSelfCheck = vi.fn()
    const m = await mountRow(makeSource({ hasSelfTest: false }), { onSelfCheck })
    mounted.push(m.unmount)
    const btn = m.container.querySelector<HTMLButtonElement>('[aria-label="自检 源甲"]')
    expect(btn?.disabled).toBe(true)
  })

  it('自检结果按五态徽标展示，dead 显示源异常', async () => {
    const m = await mountRow(makeSource(), {
      check: {
        phase: 'done',
        outcome: { source: 'src-a', state: 'dead', count: 0, rawCount: 3, dropped: 3, reason: '规则失效', latencyMs: 1 },
      },
    })
    mounted.push(m.unmount)
    expect(m.container.textContent).toContain('源异常')
    expect(m.container.textContent).toContain('规则失效')
  })
})
