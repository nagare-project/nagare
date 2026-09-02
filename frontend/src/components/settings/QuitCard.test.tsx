// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'

const endpoints = vi.hoisted(() => ({ shutdownNagare: vi.fn() }))
vi.mock('../../lib/endpoints', () => endpoints)

import { QuitCard, QuitNotice } from './QuitCard'

function button(container: HTMLElement, text: string): HTMLButtonElement | undefined {
  return Array.from(container.querySelectorAll('button')).find((b) => b.textContent === text)
}

const mounted: Array<() => Promise<void>> = []
afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.resetAllMocks()
  vi.restoreAllMocks()
})

describe('QuitCard', () => {
  it('点「退出 nagare」只弹内联确认，不调接口；「取消」回到空闲', async () => {
    const onQuit = vi.fn()
    const m = await mount(<QuitCard platform="linux" onQuit={onQuit} />)
    mounted.push(m.unmount)
    expect(m.container.textContent).toContain('Linux 没有托盘图标')
    expect(m.container.querySelector('.quit-confirm')).toBeNull()

    await act(async () => {
      button(m.container, '退出 nagare')?.click()
    })
    expect(m.container.querySelector('.quit-confirm')).not.toBeNull()
    expect(endpoints.shutdownNagare).not.toHaveBeenCalled()
    expect(onQuit).not.toHaveBeenCalled()

    await act(async () => {
      button(m.container, '取消')?.click()
    })
    expect(m.container.querySelector('.quit-confirm')).toBeNull()
    expect(button(m.container, '退出 nagare')).toBeDefined()
  })

  it('确认后调 shutdown，成功回调 onQuit', async () => {
    endpoints.shutdownNagare.mockResolvedValue(undefined)
    const onQuit = vi.fn()
    const m = await mount(<QuitCard platform="darwin" onQuit={onQuit} />)
    mounted.push(m.unmount)
    expect(m.container.textContent).toContain('菜单栏图标')
    await act(async () => {
      button(m.container, '退出 nagare')?.click()
    })
    await act(async () => {
      button(m.container, '确认退出')?.click()
    })
    expect(endpoints.shutdownNagare).toHaveBeenCalledTimes(1)
    expect(onQuit).toHaveBeenCalledTimes(1)
  })

  it('shutdown 失败：红字错误，附「已退出可直接关页」的提示，不回调 onQuit', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    endpoints.shutdownNagare.mockRejectedValue(new Error('无法连接到 nagare 后端，请确认本地服务已启动'))
    const onQuit = vi.fn()
    const m = await mount(<QuitCard onQuit={onQuit} />)
    mounted.push(m.unmount)
    expect(m.container.textContent).toContain('菜单栏 / 托盘图标')
    await act(async () => {
      button(m.container, '退出 nagare')?.click()
    })
    await act(async () => {
      button(m.container, '确认退出')?.click()
    })
    expect(onQuit).not.toHaveBeenCalled()
    const status = m.container.querySelector('.result--err[role="status"]')?.textContent ?? ''
    expect(status).toContain('无法连接到 nagare 后端')
    expect(status).toContain('直接关闭此页')
  })
})

describe('QuitNotice', () => {
  it('整页提示「nagare 已退出，可以关闭此页」', async () => {
    const m = await mount(<QuitNotice />)
    mounted.push(m.unmount)
    expect(m.container.querySelector('h1')?.textContent).toBe('nagare 已退出')
    expect(m.container.textContent).toContain('可以关闭此页')
  })
})
