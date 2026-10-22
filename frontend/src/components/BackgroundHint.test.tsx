// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../test/harness'
import { installLocalStorage, memoryStorage } from '../test/storage'
import { BACKGROUND_HINT_DISMISSED_KEY, BackgroundHint, hintText } from './BackgroundHint'

const mounted: Array<() => Promise<void>> = []

beforeEach(() => {
  installLocalStorage()
})

afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.restoreAllMocks()
})

describe('BackgroundHint', () => {
  it('设置未加载（mode=null）时不渲染', async () => {
    const m = await mount(<BackgroundHint mode={null} platform={null} />)
    mounted.push(m.unmount)
    expect(m.container.querySelector('.background-hint')).toBeNull()
  })

  it('Dock 形态：提示 Dock 图标与 ⌘Q', async () => {
    const m = await mount(<BackgroundHint mode="dock" platform="darwin" />)
    mounted.push(m.unmount)
    const text = m.container.textContent ?? ''
    expect(text).toContain('不会停止 nagare')
    expect(text).toContain('Dock')
    expect(text).toContain('⌘Q')
  })

  it('「知道了」后隐藏并记进 localStorage；下次挂载不再出现', async () => {
    const m = await mount(<BackgroundHint mode="tray" platform="windows" />)
    mounted.push(m.unmount)
    await act(async () => {
      m.container.querySelector<HTMLButtonElement>('.background-hint-dismiss')?.click()
    })
    expect(m.container.querySelector('.background-hint')).toBeNull()
    expect(window.localStorage.getItem(BACKGROUND_HINT_DISMISSED_KEY)).toBe('1')

    const again = await mount(<BackgroundHint mode="tray" platform="windows" />)
    mounted.push(again.unmount)
    expect(again.container.querySelector('.background-hint')).toBeNull()
  })

  it('localStorage 抛异常时照常显示、关闭也不炸', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    installLocalStorage({
      ...memoryStorage(),
      getItem: () => {
        throw new Error('SecurityError')
      },
      setItem: () => {
        throw new Error('SecurityError')
      },
    })
    const m = await mount(<BackgroundHint mode="menubar" platform="darwin" />)
    mounted.push(m.unmount)
    expect(m.container.querySelector('.background-hint')).not.toBeNull()
    await act(async () => {
      m.container.querySelector<HTMLButtonElement>('.background-hint-dismiss')?.click()
    })
    expect(m.container.querySelector('.background-hint')).toBeNull()
  })
})

describe('hintText', () => {
  it('每种形态都说清去哪里找、怎么退出，且不把「菜单栏」硬塞给非 mac', () => {
    expect(hintText('menubar', 'darwin')).toContain('菜单栏')
    expect(hintText('tray', 'windows')).toContain('^')
    expect(hintText('tray', 'windows')).not.toContain('菜单栏')
    expect(hintText('tray', 'linux')).toContain('设置页')
    expect(hintText('none', 'linux')).toContain('Ctrl+C')
  })

  it('桌面窗口里：说「关闭窗口」不说「标签页」，后台下载照旧，Dock / 菜单栏「流」重新打开，⌘Q 退出', () => {
    const text = hintText('window', 'darwin', true)
    expect(text).toBe('关闭窗口不会退出 nagare，正在进行的下载会继续。点 Dock 图标或菜单栏的「流」可以重新打开，按 ⌘Q 退出。')
    expect(text).not.toContain('标签页')
    expect(text).not.toContain('浏览器')
    expect(text).not.toContain('做种')
  })

  it('Windows 的桌面窗口：指托盘，不提 Dock 与 ⌘Q', () => {
    const text = hintText('window', 'windows', true)
    expect(text).toContain('关闭窗口不会退出 nagare')
    expect(text).toContain('托盘')
    expect(text).toContain('^')
    expect(text).not.toContain('Dock')
    expect(text).not.toContain('⌘')
  })

  it('有桌面窗口、这一页却开在外部浏览器里：关的是标签页，回去找窗口，不教在浏览器里按 ⌘Q', () => {
    const mac = hintText('window', 'darwin', false)
    expect(mac).toContain('关闭这个标签页不会停止 nagare')
    expect(mac).toContain('nagare 的窗口')
    expect(mac).toContain('「流」')
    expect(mac).not.toContain('⌘Q')
    const win = hintText('window', 'windows', false)
    expect(win).toContain('关闭这个标签页不会停止 nagare')
    expect(win).toContain('托盘')
    expect(win).not.toContain('Dock')
  })

  it('不是 window 形态时 inWindow 不改措辞（没有窗口可言）', () => {
    expect(hintText('dock', 'darwin', true)).toBe(hintText('dock', 'darwin', false))
  })
})

describe('BackgroundHint 在桌面窗口里', () => {
  it('window 形态 + 在窗口里：渲染窗口版文案', async () => {
    const m = await mount(<BackgroundHint mode="window" platform="darwin" inWindow />)
    mounted.push(m.unmount)
    const text = m.container.querySelector('.background-hint-text')?.textContent ?? ''
    expect(text).toContain('关闭窗口不会退出 nagare')
    expect(text).not.toContain('标签页')
  })

  it('不传 inWindow 时按 User-Agent 判断', async () => {
    vi.spyOn(window.navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Macintosh) AppleWebKit/605.1.15 (KHTML, like Gecko) NagareDesktop')
    const inside = await mount(<BackgroundHint mode="window" platform="darwin" />)
    mounted.push(inside.unmount)
    expect(inside.container.textContent).toContain('关闭窗口不会退出 nagare')

    vi.spyOn(window.navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Macintosh) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15')
    const tab = await mount(<BackgroundHint mode="window" platform="darwin" />)
    mounted.push(tab.unmount)
    expect(tab.container.textContent).toContain('关闭这个标签页不会停止 nagare')
  })
})
