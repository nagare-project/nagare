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
})
