// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { RulesInfo } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { RulesConfigForm } from './RulesConfigForm'

function makeRules(overrides: Partial<RulesInfo> = {}): RulesInfo {
  return {
    remoteUrl: '',
    localDir: '',
    dir: '/tmp/rules',
    loaded: 0,
    errors: [],
    lastLoadedAt: null,
    lastSyncAt: null,
    ...overrides,
  }
}

const mounted: Array<() => Promise<void>> = []
afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
})

async function submit(container: HTMLElement) {
  await act(async () => {
    container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}

describe('RulesConfigForm', () => {
  it('非 https 的仓库地址在前端就拦下，不调用保存', async () => {
    const onSave = vi.fn()
    const m = await mount(
      <RulesConfigForm rules={makeRules({ remoteUrl: 'http://rules.example' })} onSave={onSave} onSync={vi.fn()} onReload={vi.fn()} />,
    )
    mounted.push(m.unmount)
    await submit(m.container)
    expect(onSave).not.toHaveBeenCalled()
    expect(m.container.querySelector('[role="status"]')?.textContent).toContain('https://')
  })

  it('合法地址提交后调用保存并显示加载条数', async () => {
    const onSave = vi.fn(async () => makeRules({ remoteUrl: 'https://rules.example', loaded: 6 }))
    const m = await mount(
      <RulesConfigForm rules={makeRules({ remoteUrl: 'https://rules.example', localDir: '/abs/dir' })} onSave={onSave} onSync={vi.fn()} onReload={vi.fn()} />,
    )
    mounted.push(m.unmount)
    await submit(m.container)
    expect(onSave).toHaveBeenCalledWith({ remoteUrl: 'https://rules.example', localDir: '/abs/dir' })
    expect(m.container.querySelector('[role="status"]')?.textContent).toContain('6 条规则')
  })

  it('placeholder 不给任何示例站点或地址', async () => {
    const m = await mount(
      <RulesConfigForm rules={makeRules()} onSave={vi.fn()} onSync={vi.fn()} onReload={vi.fn()} />,
    )
    mounted.push(m.unmount)
    for (const input of Array.from(m.container.querySelectorAll('input'))) {
      expect(input.getAttribute('placeholder') ?? '').not.toMatch(/https?:\/\/[a-z]/i)
    }
  })

  it('未配置远端时「同步规则」禁用；同步结果与规则级错误都要展示', async () => {
    const onSync = vi.fn(async () => ({ added: 1, updated: 0, removed: 2, errors: ['bad.yaml: 规则无效'] }))
    const m = await mount(
      <RulesConfigForm rules={makeRules({ remoteUrl: 'https://rules.example' })} onSave={vi.fn()} onSync={onSync} onReload={vi.fn()} />,
    )
    mounted.push(m.unmount)
    const syncBtn = Array.from(m.container.querySelectorAll('button')).find((b) => b.textContent === '同步规则')
    expect(syncBtn?.disabled).toBe(false)
    await act(async () => {
      syncBtn?.click()
    })
    expect(m.container.querySelector('[role="status"]')?.textContent).toContain('新增 1')
    expect(m.container.querySelector('[role="alert"]')?.textContent).toContain('bad.yaml')

    const m2 = await mount(<RulesConfigForm rules={makeRules()} onSave={vi.fn()} onSync={vi.fn()} onReload={vi.fn()} />)
    mounted.push(m2.unmount)
    const disabledSync = Array.from(m2.container.querySelectorAll('button')).find((b) => b.textContent === '同步规则')
    expect(disabledSync?.disabled).toBe(true)
  })
})
