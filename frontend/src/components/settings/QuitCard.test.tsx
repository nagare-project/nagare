// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { getByRole, queryByRole } from '../../test/queries'
import { mount } from '../../test/harness'

const endpoints = vi.hoisted(() => ({ shutdownNagare: vi.fn() }))
vi.mock('../../lib/endpoints', () => endpoints)

import { QuitCard, QuitNotice, quitNote } from './QuitCard'

function button(container: HTMLElement, name: string): HTMLButtonElement | undefined {
  return (queryByRole(container, 'button', { name }) as HTMLButtonElement | null) ?? undefined
}

const confirmGroup = (container: HTMLElement) => queryByRole(container, 'group', { name: '确认退出' })

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
    expect(confirmGroup(m.container)).toBeNull()

    await act(async () => {
      button(m.container, '退出 nagare')?.click()
    })
    expect(confirmGroup(m.container)).not.toBeNull()
    expect(endpoints.shutdownNagare).not.toHaveBeenCalled()
    expect(onQuit).not.toHaveBeenCalled()

    await act(async () => {
      button(m.container, '取消')?.click()
    })
    expect(confirmGroup(m.container)).toBeNull()
    expect(button(m.container, '退出 nagare')).toBeDefined()
  })

  // 「退出 nagare」按下去就被确认区换掉了：焦点不能跟着掉到 <body>，键盘用户得能接着操作
  it('确认区出来时焦点在「取消」，取消后回到「退出 nagare」', async () => {
    const m = await mount(<QuitCard platform="darwin" onQuit={vi.fn()} />)
    mounted.push(m.unmount)
    await act(async () => {
      getByRole(m.container, 'button', { name: '退出 nagare' }).click()
    })
    expect(getByRole(m.container, 'group', { name: '确认退出' })).not.toBeNull()
    expect(document.activeElement).toBe(getByRole(m.container, 'button', { name: '取消' }))

    await act(async () => {
      getByRole(m.container, 'button', { name: '取消' }).click()
    })
    expect(document.activeElement).toBe(getByRole(m.container, 'button', { name: '退出 nagare' }))
  })

  it('确认区里按 Esc 等于「取消」：确认区收起，焦点回到「退出 nagare」，不发退出请求', async () => {
    const m = await mount(<QuitCard platform="darwin" onQuit={vi.fn()} />)
    mounted.push(m.unmount)
    await act(async () => {
      getByRole(m.container, 'button', { name: '退出 nagare' }).click()
    })
    // 焦点挪到「确认退出」上再按 Esc 也一样（Esc 落在确认区的哪个按钮上都算）
    getByRole(m.container, 'button', { name: '确认退出' }).focus()
    await act(async () => {
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(confirmGroup(m.container)).toBeNull()
    expect(document.activeElement).toBe(getByRole(m.container, 'button', { name: '退出 nagare' }))
    expect(endpoints.shutdownNagare).not.toHaveBeenCalled()
  })

  it('请求中：「正在退出」不可用但焦点留在它身上（不掉到 <body>），Esc 与再点都不起作用', async () => {
    endpoints.shutdownNagare.mockReturnValue(new Promise(() => {}))
    const onQuit = vi.fn()
    const m = await mount(<QuitCard platform="darwin" onQuit={onQuit} />)
    mounted.push(m.unmount)
    await act(async () => {
      getByRole(m.container, 'button', { name: '退出 nagare' }).click()
    })
    const confirm = getByRole(m.container, 'button', { name: '确认退出' }) as HTMLButtonElement
    confirm.focus()
    await act(async () => confirm.click())
    const busy = getByRole(m.container, 'button', { name: '正在退出 …' }) as HTMLButtonElement
    expect(busy).toBe(confirm)
    expect(busy.disabled).toBe(false)
    expect(busy.getAttribute('aria-disabled')).toBe('true')
    expect(document.activeElement).toBe(busy)
    await act(async () => {
      busy.click()
      busy.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(endpoints.shutdownNagare).toHaveBeenCalledTimes(1)
    expect(confirmGroup(m.container)).not.toBeNull()
  })

  it('退出失败：说出原因，焦点回到「退出 nagare」', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    endpoints.shutdownNagare.mockRejectedValue(new Error('后端没有响应'))
    const m = await mount(<QuitCard platform="darwin" onQuit={vi.fn()} />)
    mounted.push(m.unmount)
    await act(async () => {
      getByRole(m.container, 'button', { name: '退出 nagare' }).click()
    })
    await act(async () => {
      getByRole(m.container, 'button', { name: '确认退出' }).click()
    })
    expect(getByRole(m.container, 'status').textContent).toContain('后端没有响应')
    expect(document.activeElement).toBe(getByRole(m.container, 'button', { name: '退出 nagare' }))
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
    const status = getByRole(m.container, 'status').textContent ?? ''
    expect(status).toContain('无法连接到 nagare 后端')
    expect(status).toContain('直接关闭此页')
  })
})

describe('QuitCard 在桌面窗口里', () => {
  it('说「关闭窗口」不说「浏览器标签页」；macOS 指 ⌘Q 与菜单栏「流」；确认里不提 mpv', async () => {
    const m = await mount(<QuitCard platform="darwin" inWindow onQuit={vi.fn()} />)
    mounted.push(m.unmount)
    const text = m.container.textContent ?? ''
    expect(text).toContain('关闭窗口不会结束 nagare 的后台进程。')
    expect(text).toContain('⌘Q')
    expect(text).toContain('「流」')
    expect(text).not.toContain('浏览器标签页')
    await act(async () => {
      button(m.container, '退出 nagare')?.click()
    })
    const confirm = confirmGroup(m.container)!
    // 焦点落在「取消」上：问题本身挂在分组的描述上，读屏才念得到
    expect(document.getElementById(confirm.getAttribute('aria-describedby') ?? '')?.textContent).toBe('确定退出 nagare 吗？正在播放的视频也会停止。')
  })

  it('Windows 的桌面窗口：指托盘，不提 ⌘Q', () => {
    expect(quitNote('windows', true)).toBe('关闭窗口不会结束 nagare 的后台进程。Windows 也可以从托盘图标退出。')
  })

  it('外部浏览器里照旧说标签页', () => {
    expect(quitNote('darwin', false)).toBe('关闭浏览器标签页不会结束 nagare 的后台进程。macOS 也可以从菜单栏图标退出。')
  })
})

describe('QuitNotice', () => {
  it('整页提示「nagare 已退出，可以关闭此页」', async () => {
    const m = await mount(<QuitNotice />)
    mounted.push(m.unmount)
    expect(getByRole(m.container, 'heading', { level: 1, name: 'nagare 已退出' })).toBeDefined()
    expect(m.container.textContent).toContain('可以关闭此页')
  })
})
