// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UpdateView } from '../lib/endpoints'
import { mount } from '../test/harness'
import { installLocalStorage, memoryStorage } from '../test/storage'
import { IGNORED_VERSION_KEY, UpdateBanner } from './UpdateBanner'

const NEWER: UpdateView = {
  enabled: true,
  current: '0.1.0',
  latest: 'v0.2.0',
  available: true,
  url: 'https://github.com/nagare-project/nagare/releases/tag/v0.2.0',
  checkedAt: 1_756_800_000,
  error: '',
}

const mounted: Array<() => Promise<void>> = []

beforeEach(() => {
  installLocalStorage()
})

afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.restoreAllMocks()
})

describe('UpdateBanner', () => {
  it('available：显示版本、下载外链（新标签页）与「忽略此版本」', async () => {
    const m = await mount(<UpdateBanner view={NEWER} />)
    mounted.push(m.unmount)
    expect(m.container.querySelector('.update-banner')).not.toBeNull()
    expect(m.container.textContent).toContain('新版本 v0.2.0 可用')
    expect(m.container.textContent).toContain('当前 v0.1.0')
    const a = m.container.querySelector('a.update-banner-link')
    expect(a?.getAttribute('href')).toBe(NEWER.url)
    expect(a?.getAttribute('target')).toBe('_blank')
    expect(a?.getAttribute('rel')).toContain('noopener')
  })

  it('未加载 / 没有新版本 → 什么都不渲染', async () => {
    const m1 = await mount(<UpdateBanner view={null} />)
    mounted.push(m1.unmount)
    expect(m1.container.querySelector('.update-banner')).toBeNull()

    const m2 = await mount(<UpdateBanner view={{ ...NEWER, available: false }} />)
    mounted.push(m2.unmount)
    expect(m2.container.querySelector('.update-banner')).toBeNull()
  })

  it('「忽略此版本」后隐藏并记进 localStorage；下次挂载同一版本仍不出现，换版本又出现', async () => {
    const m = await mount(<UpdateBanner view={NEWER} />)
    mounted.push(m.unmount)
    await act(async () => {
      m.container.querySelector<HTMLButtonElement>('.update-banner-ignore')?.click()
    })
    expect(m.container.querySelector('.update-banner')).toBeNull()
    expect(window.localStorage.getItem(IGNORED_VERSION_KEY)).toBe('v0.2.0')

    const again = await mount(<UpdateBanner view={NEWER} />)
    mounted.push(again.unmount)
    expect(again.container.querySelector('.update-banner')).toBeNull()

    const newer = await mount(<UpdateBanner view={{ ...NEWER, latest: 'v0.3.0' }} />)
    mounted.push(newer.unmount)
    expect(newer.container.textContent).toContain('v0.3.0')
  })

  it('localStorage 抛异常时照常显示、忽略也不炸', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    installLocalStorage({
      ...memoryStorage(),
      getItem: () => {
        throw new Error('SecurityError')
      },
      setItem: () => {
        throw new Error('QuotaExceededError')
      },
    })
    const m = await mount(<UpdateBanner view={NEWER} />)
    mounted.push(m.unmount)
    expect(m.container.querySelector('.update-banner')).not.toBeNull()
    await act(async () => {
      m.container.querySelector<HTMLButtonElement>('.update-banner-ignore')?.click()
    })
    expect(m.container.querySelector('.update-banner')).toBeNull()
  })

  it('非 http(s) 的 url 不渲染成链接', async () => {
    const m = await mount(<UpdateBanner view={{ ...NEWER, url: 'javascript:alert(1)' }} />)
    mounted.push(m.unmount)
    expect(m.container.querySelector('.update-banner')).not.toBeNull()
    expect(m.container.querySelector('a')).toBeNull()
  })
})
