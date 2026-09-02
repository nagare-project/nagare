// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { UseUpdateResult } from '../../hooks/useUpdate'
import type { UpdateView } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { UpdateCard } from './UpdateCard'

const IDLE: UpdateView = {
  enabled: true,
  current: '0.1.0',
  latest: '',
  available: false,
  url: '',
  checkedAt: null,
  error: '',
}

const NEWER: UpdateView = {
  ...IDLE,
  latest: 'v0.2.0',
  available: true,
  url: 'https://github.com/nagare-project/nagare/releases/tag/v0.2.0',
  checkedAt: new Date(2026, 8, 2, 10, 30).getTime(),
}

function fakeUpdate(overrides: Partial<UseUpdateResult> = {}): UseUpdateResult {
  return {
    state: { phase: 'ready', data: IDLE },
    reload: vi.fn().mockResolvedValue(undefined),
    check: vi.fn().mockResolvedValue(IDLE),
    setEnabled: vi.fn().mockResolvedValue({ ...IDLE, enabled: false }),
    ...overrides,
  }
}

function button(container: HTMLElement, text: string): HTMLButtonElement {
  const found = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === text)
  if (found === undefined) throw new Error(`找不到按钮「${text}」`)
  return found
}

const mounted: Array<() => Promise<void>> = []
afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.restoreAllMocks()
})

describe('UpdateCard', () => {
  it('未检查过：版本带 v 前缀、最新为 —、上次检查为「尚未检查」', async () => {
    const m = await mount(<UpdateCard update={fakeUpdate()} />)
    mounted.push(m.unmount)
    const dds = Array.from(m.container.querySelectorAll('.kv-list dd')).map((dd) => dd.textContent)
    expect(dds).toEqual(['v0.1.0', '—', '尚未检查'])
    expect(m.container.querySelector('.update-error')).toBeNull()
    expect(m.container.textContent).toContain('每天最多向 GitHub 查询一次')
  })

  it('有新版本：徽标 + 下载外链 + 上次检查时间', async () => {
    const m = await mount(<UpdateCard update={fakeUpdate({ state: { phase: 'ready', data: NEWER } })} />)
    mounted.push(m.unmount)
    expect(m.container.querySelector('.update-badge')?.textContent).toBe('新版本')
    const a = m.container.querySelector('a.update-download')
    expect(a?.getAttribute('href')).toBe(NEWER.url)
    expect(a?.getAttribute('rel')).toContain('noopener')
    expect(m.container.textContent).toContain('2026-09-02 10:30')
  })

  it('后端记录的检查失败原因用红字展示', async () => {
    const m = await mount(
      <UpdateCard
        update={fakeUpdate({ state: { phase: 'ready', data: { ...IDLE, error: 'GitHub 请求超时' } } })}
      />,
    )
    mounted.push(m.unmount)
    expect(m.container.querySelector('.update-error')?.textContent).toContain('GitHub 请求超时')
  })

  it('「立即检查」调 check() 并给出结论', async () => {
    const check = vi.fn().mockResolvedValue(NEWER)
    const m = await mount(<UpdateCard update={fakeUpdate({ check })} />)
    mounted.push(m.unmount)
    await act(async () => {
      button(m.container, '立即检查').click()
    })
    expect(check).toHaveBeenCalledTimes(1)
    expect(m.container.querySelector('.result--ok[role="status"]')?.textContent).toBe(
      '发现新版本 v0.2.0',
    )
  })

  it('「立即检查」请求失败：红字中文错误', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const check = vi.fn().mockRejectedValue(new Error('无法连接到 nagare 后端，请确认本地服务已启动'))
    const m = await mount(<UpdateCard update={fakeUpdate({ check })} />)
    mounted.push(m.unmount)
    await act(async () => {
      button(m.container, '立即检查').click()
    })
    expect(m.container.querySelector('.result--err[role="status"]')?.textContent).toBe(
      '无法连接到 nagare 后端，请确认本地服务已启动',
    )
  })

  it('开关取反当前 enabled 并调 setEnabled()', async () => {
    const setEnabled = vi.fn().mockResolvedValue({ ...IDLE, enabled: false })
    const m = await mount(<UpdateCard update={fakeUpdate({ setEnabled })} />)
    mounted.push(m.unmount)
    const sw = m.container.querySelector<HTMLButtonElement>('[role="switch"]')
    expect(sw?.getAttribute('aria-checked')).toBe('true')
    await act(async () => {
      sw?.click()
    })
    expect(setEnabled).toHaveBeenCalledExactlyOnceWith(false)
    expect(m.container.querySelector('.result--ok[role="status"]')?.textContent).toBe('已关闭自动检查')
  })

  it('加载失败：显示原因与重试', async () => {
    const reload = vi.fn().mockResolvedValue(undefined)
    const m = await mount(
      <UpdateCard update={fakeUpdate({ state: { phase: 'error', message: '后端挂了' }, reload })} />,
    )
    mounted.push(m.unmount)
    expect(m.container.textContent).toContain('后端挂了')
    await act(async () => {
      button(m.container, '重试').click()
    })
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
