// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SelfUpdateState, UseSelfUpdateResult } from '../hooks/useSelfUpdate'
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
  selfUpdate: { supported: true, channel: 'app-bundle', target: '/Applications/Nagare.app' },
}

/** 包管理器装的：提示条保持 M4 阶段 A 的样子，只有下载链接 */
const BY_PACKAGE: UpdateView = {
  ...NEWER,
  selfUpdate: {
    supported: false,
    channel: 'package',
    reason: '请执行 sudo apt upgrade nagare 升级。',
    target: '/usr/bin/nagare',
  },
}

function fakeSelfUpdate(
  state: SelfUpdateState = { phase: 'idle' },
  elapsedSec = 0,
): UseSelfUpdateResult {
  return {
    state,
    elapsedSec,
    busy: state.phase === 'applying' || state.phase === 'restarting',
    start: vi.fn(),
  }
}

function maybeButton(container: HTMLElement, text: string): HTMLButtonElement | undefined {
  return Array.from(container.querySelectorAll('button')).find((b) => b.textContent === text)
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
    const m = await mount(<UpdateBanner view={NEWER} selfUpdate={fakeSelfUpdate()} />)
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
    const m1 = await mount(<UpdateBanner view={null} selfUpdate={fakeSelfUpdate()} />)
    mounted.push(m1.unmount)
    expect(m1.container.querySelector('.update-banner')).toBeNull()

    const m2 = await mount(
      <UpdateBanner view={{ ...NEWER, available: false }} selfUpdate={fakeSelfUpdate()} />,
    )
    mounted.push(m2.unmount)
    expect(m2.container.querySelector('.update-banner')).toBeNull()
  })

  it('「忽略此版本」后隐藏并记进 localStorage；下次挂载同一版本仍不出现，换版本又出现', async () => {
    const m = await mount(<UpdateBanner view={NEWER} selfUpdate={fakeSelfUpdate()} />)
    mounted.push(m.unmount)
    await act(async () => {
      m.container.querySelector<HTMLButtonElement>('.update-banner-ignore')?.click()
    })
    expect(m.container.querySelector('.update-banner')).toBeNull()
    expect(window.localStorage.getItem(IGNORED_VERSION_KEY)).toBe('v0.2.0')

    const again = await mount(<UpdateBanner view={NEWER} selfUpdate={fakeSelfUpdate()} />)
    mounted.push(again.unmount)
    expect(again.container.querySelector('.update-banner')).toBeNull()

    const newer = await mount(
      <UpdateBanner view={{ ...NEWER, latest: 'v0.3.0' }} selfUpdate={fakeSelfUpdate()} />,
    )
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
    const m = await mount(<UpdateBanner view={NEWER} selfUpdate={fakeSelfUpdate()} />)
    mounted.push(m.unmount)
    expect(m.container.querySelector('.update-banner')).not.toBeNull()
    await act(async () => {
      m.container.querySelector<HTMLButtonElement>('.update-banner-ignore')?.click()
    })
    expect(m.container.querySelector('.update-banner')).toBeNull()
  })

  it('非 http(s) 的 url 不渲染成链接', async () => {
    const m = await mount(
      <UpdateBanner view={{ ...NEWER, url: 'javascript:alert(1)' }} selfUpdate={fakeSelfUpdate()} />,
    )
    mounted.push(m.unmount)
    expect(m.container.querySelector('.update-banner')).not.toBeNull()
    expect(m.container.querySelector('a')).toBeNull()
  })
})

describe('UpdateBanner · 一键更新', () => {
  it('可自更新：在「去下载」旁边多一个「立即更新」，点击调 start()', async () => {
    const selfUpdate = fakeSelfUpdate()
    const m = await mount(<UpdateBanner view={NEWER} selfUpdate={selfUpdate} />)
    mounted.push(m.unmount)

    const apply = m.container.querySelector<HTMLButtonElement>('.update-banner-apply')
    expect(apply?.textContent).toBe('立即更新')
    await act(async () => {
      apply?.click()
    })
    expect(selfUpdate.start).toHaveBeenCalledTimes(1)
  })

  it('不可自更新：保持原样，只有下载链接与「忽略此版本」', async () => {
    const m = await mount(<UpdateBanner view={BY_PACKAGE} selfUpdate={fakeSelfUpdate()} />)
    mounted.push(m.unmount)
    expect(m.container.querySelector('.update-banner-apply')).toBeNull()
    expect(m.container.querySelector('a.update-banner-link')).not.toBeNull()
    expect(maybeButton(m.container, '忽略此版本')).toBeDefined()
    expect(m.container.textContent).toContain('新版本 v0.2.0 可用')
  })

  it('更新中：文案换成阶段说明，按钮禁用；秒数对读屏隐藏（整条是 live region）', async () => {
    const m = await mount(
      <UpdateBanner view={NEWER} selfUpdate={fakeSelfUpdate({ phase: 'applying' }, 12)} />,
    )
    mounted.push(m.unmount)

    const text = m.container.querySelector('.update-banner-text')
    expect(text?.textContent).toContain('正在下载并校验更新包')
    expect(text?.textContent).not.toContain('新版本 v0.2.0 可用')
    expect(text?.querySelector('[aria-hidden="true"]')?.textContent).toBe('（已 12 秒）')

    const apply = m.container.querySelector<HTMLButtonElement>('.update-banner-apply')
    expect(apply?.textContent).toBe('更新中 …')
    expect(apply?.disabled).toBe(true)
    expect(m.container.querySelector('.update-banner-dot')).not.toBeNull()
  })

  // 点了「忽略」提示条会连同进度一起消失，而后端还在下载 —— 用户会以为自己取消了
  it('更新中：不给「忽略此版本」，换成一句说明原因的文字', async () => {
    const m = await mount(
      <UpdateBanner view={NEWER} selfUpdate={fakeSelfUpdate({ phase: 'restarting', version: '0.2.0' }, 3)} />,
    )
    mounted.push(m.unmount)

    expect(m.container.querySelector('.update-banner-ignore')).toBeNull()
    expect(m.container.querySelector('.update-banner-hint')?.textContent).toBe(
      '更新进行中，暂时不能忽略此版本',
    )
  })

  it('更新已装好（done / timeout）：不再给「立即更新」，忽略按钮回来', async () => {
    const done = await mount(
      <UpdateBanner view={NEWER} selfUpdate={fakeSelfUpdate({ phase: 'done', version: '0.2.0' })} />,
    )
    mounted.push(done.unmount)
    expect(done.container.querySelector('.update-banner-apply')).toBeNull()
    expect(done.container.textContent).toContain('已更新到 v0.2.0')

    const timeout = await mount(
      <UpdateBanner
        view={NEWER}
        selfUpdate={fakeSelfUpdate({ phase: 'timeout', version: '0.2.0' })}
      />,
    )
    mounted.push(timeout.unmount)
    expect(timeout.container.querySelector('.update-banner-apply')).toBeNull()
    expect(timeout.container.textContent).toContain('请手动重新打开 nagare')
    // 更新已经结束，忽略按钮不该继续被锁着
    expect(timeout.container.querySelector('.update-banner-ignore')).not.toBeNull()
  })

  it('更新失败：提示条就地显示中文错误，按钮变「重试」', async () => {
    const selfUpdate = fakeSelfUpdate({ phase: 'error', message: '下载更新包失败：连接超时' })
    const m = await mount(<UpdateBanner view={NEWER} selfUpdate={selfUpdate} />)
    mounted.push(m.unmount)

    const text = m.container.querySelector('.update-banner-text')
    expect(text?.textContent).toBe('下载更新包失败：连接超时')
    expect(text?.className).toContain('result--err')

    const apply = m.container.querySelector<HTMLButtonElement>('.update-banner-apply')
    expect(apply?.textContent).toBe('重试')
    expect(apply?.disabled).toBe(false)
    await act(async () => {
      apply?.click()
    })
    expect(selfUpdate.start).toHaveBeenCalledTimes(1)
  })
})
