// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SelfUpdateState, UseSelfUpdateResult } from '../../hooks/useSelfUpdate'
import type { UseUpdateResult } from '../../hooks/useUpdate'
import type { SelfUpdateInfo, UpdateView } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { UpdateCard } from './UpdateCard'

/** macOS 的 .app：能一键更新 */
const SUPPORTED: SelfUpdateInfo = {
  supported: true,
  channel: 'app-bundle',
  target: '/Applications/Nagare.app',
}

/** deb 装的：归包管理器管，界面只能提示去用 apt */
const BY_PACKAGE: SelfUpdateInfo = {
  supported: false,
  channel: 'package',
  reason: 'nagare 是用 deb 包安装的，请执行 sudo apt upgrade nagare 升级。',
  target: '/usr/bin/nagare',
}

const IDLE: UpdateView = {
  enabled: true,
  current: '0.1.0',
  latest: '',
  available: false,
  url: '',
  checkedAt: null,
  error: '',
  selfUpdate: SUPPORTED,
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

function button(container: HTMLElement, text: string): HTMLButtonElement {
  const found = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === text)
  if (found === undefined) throw new Error(`找不到按钮「${text}」`)
  return found
}

function maybeButton(container: HTMLElement, text: string): HTMLButtonElement | undefined {
  return Array.from(container.querySelectorAll('button')).find((b) => b.textContent === text)
}

const mounted: Array<() => Promise<void>> = []
afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.restoreAllMocks()
})

describe('UpdateCard · 版本信息', () => {
  it('未检查过：版本带 v 前缀、最新为 —、上次检查为「尚未检查」、自更新可用', async () => {
    const m = await mount(<UpdateCard update={fakeUpdate()} selfUpdate={fakeSelfUpdate()} />)
    mounted.push(m.unmount)
    const dds = Array.from(m.container.querySelectorAll('.kv-list dd')).map((dd) => dd.textContent)
    expect(dds).toEqual(['v0.1.0', '—', '尚未检查', '可用（macOS 应用包）'])
    expect(m.container.querySelector('.update-error')).toBeNull()
    expect(m.container.textContent).toContain('每天最多向 GitHub 查询一次')
    // 没有新版本时不渲染一键更新区块：那时它没有任何可说的
    expect(m.container.querySelector('.self-update')).toBeNull()
  })

  it('有新版本：徽标 + 下载外链 + 上次检查时间', async () => {
    const m = await mount(
      <UpdateCard
        update={fakeUpdate({ state: { phase: 'ready', data: NEWER } })}
        selfUpdate={fakeSelfUpdate()}
      />,
    )
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
        selfUpdate={fakeSelfUpdate()}
      />,
    )
    mounted.push(m.unmount)
    expect(m.container.querySelector('.update-error')?.textContent).toContain('GitHub 请求超时')
  })

  it('加载失败：显示原因与重试', async () => {
    const reload = vi.fn().mockResolvedValue(undefined)
    const m = await mount(
      <UpdateCard
        update={fakeUpdate({ state: { phase: 'error', message: '后端挂了' }, reload })}
        selfUpdate={fakeSelfUpdate()}
      />,
    )
    mounted.push(m.unmount)
    expect(m.container.textContent).toContain('后端挂了')
    await act(async () => {
      button(m.container, '重试').click()
    })
    expect(reload).toHaveBeenCalledTimes(1)
  })
})

describe('UpdateCard · 检查与开关', () => {
  it('「立即检查」调 check() 并给出结论', async () => {
    const check = vi.fn().mockResolvedValue(NEWER)
    const m = await mount(
      <UpdateCard update={fakeUpdate({ check })} selfUpdate={fakeSelfUpdate()} />,
    )
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
    const m = await mount(
      <UpdateCard update={fakeUpdate({ check })} selfUpdate={fakeSelfUpdate()} />,
    )
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
    const m = await mount(
      <UpdateCard update={fakeUpdate({ setEnabled })} selfUpdate={fakeSelfUpdate()} />,
    )
    mounted.push(m.unmount)
    const sw = m.container.querySelector<HTMLButtonElement>('[role="switch"]')
    expect(sw?.getAttribute('aria-checked')).toBe('true')
    await act(async () => {
      sw?.click()
    })
    expect(setEnabled).toHaveBeenCalledExactlyOnceWith(false)
    expect(m.container.querySelector('.result--ok[role="status"]')?.textContent).toBe('已关闭自动检查')
  })
})

describe('UpdateCard · 一键更新', () => {
  const withNewer = (data: Partial<UpdateView> = {}) =>
    fakeUpdate({ state: { phase: 'ready', data: { ...NEWER, ...data } } })

  it('可自更新：出现「立即更新」，点击调 start()，说明里写清会替换什么', async () => {
    const selfUpdate = fakeSelfUpdate()
    const m = await mount(<UpdateCard update={withNewer()} selfUpdate={selfUpdate} />)
    mounted.push(m.unmount)

    const note = m.container.querySelector('.self-update-note')?.textContent ?? ''
    expect(note).toContain('验证 minisign 签名')
    expect(note).toContain('/Applications/Nagare.app')
    expect(note).toContain('自动重启并刷新页面')

    await act(async () => {
      button(m.container, '立即更新').click()
    })
    expect(selfUpdate.start).toHaveBeenCalledTimes(1)
  })

  it('包管理器装的（supported=false）：没有「立即更新」，显示后端原因与下载页链接', async () => {
    const m = await mount(
      <UpdateCard update={withNewer({ selfUpdate: BY_PACKAGE })} selfUpdate={fakeSelfUpdate()} />,
    )
    mounted.push(m.unmount)

    expect(maybeButton(m.container, '立即更新')).toBeUndefined()
    expect(m.container.querySelector('.self-update-unsupported')?.textContent).toBe(
      BY_PACKAGE.reason,
    )
    const link = m.container.querySelector('a.self-update-link')
    expect(link?.getAttribute('href')).toBe(NEWER.url)
    expect(link?.textContent).toContain('手动更新')
    // 「自更新」这一行常驻，用户不用点进去才知道自己这个装法不行
    expect(m.container.textContent).toContain('不可用（系统包管理器）')
  })

  it('后端没给 reason 时按安装方式兜底，不留一个「按钮消失了」的哑谜', async () => {
    const m = await mount(
      <UpdateCard
        update={withNewer({
          selfUpdate: { supported: false, channel: 'unknown', target: '/opt/nagare/nagare' },
        })}
        selfUpdate={fakeSelfUpdate()}
      />,
    )
    mounted.push(m.unmount)
    expect(m.container.querySelector('.self-update-unsupported')?.textContent).toContain(
      '认不出 nagare 是怎么安装的',
    )
  })

  it('更新中：更新 / 检查 / 自动检查开关一并禁用，并给出走秒的阶段文案', async () => {
    const m = await mount(
      <UpdateCard update={withNewer()} selfUpdate={fakeSelfUpdate({ phase: 'applying' }, 42)} />,
    )
    mounted.push(m.unmount)

    expect(button(m.container, '更新中 …').disabled).toBe(true)
    expect(button(m.container, '立即检查').disabled).toBe(true)
    expect(m.container.querySelector<HTMLButtonElement>('[role="switch"]')?.disabled).toBe(true)

    const status = m.container.querySelector('.self-update-status')?.textContent ?? ''
    expect(status).toContain('正在下载并校验更新包')
    expect(status).toContain('（已 42 秒）')
    // 呼吸点：几分钟没有数字之外的变化时，它是「进程还活着」的第二个信号
    expect(m.container.querySelector('.self-update-dot')).not.toBeNull()
  })

  it('等重启期间显示新版本号与已等秒数', async () => {
    const m = await mount(
      <UpdateCard
        update={withNewer()}
        selfUpdate={fakeSelfUpdate({ phase: 'restarting', version: '0.2.0' }, 5)}
      />,
    )
    mounted.push(m.unmount)
    const status = m.container.querySelector('.self-update-status')?.textContent ?? ''
    expect(status).toContain('v0.2.0 已装好，正在等待服务重新启动')
    expect(status).toContain('（已 5 秒）')
  })

  it('更新失败：原样显示后端中文错误，「重试」与「改用下载页」都在', async () => {
    const selfUpdate = fakeSelfUpdate({
      phase: 'error',
      message: '更新包签名校验失败，已丢弃下载的文件',
    })
    const m = await mount(<UpdateCard update={withNewer()} selfUpdate={selfUpdate} />)
    mounted.push(m.unmount)

    const status = m.container.querySelector('.self-update-status')
    expect(status?.textContent).toBe('更新包签名校验失败，已丢弃下载的文件')
    expect(status?.className).toContain('result--err')

    const retry = button(m.container, '重试')
    expect(retry.disabled).toBe(false)
    await act(async () => {
      retry.click()
    })
    expect(selfUpdate.start).toHaveBeenCalledTimes(1)

    expect(m.container.querySelector('a.self-update-link')?.textContent).toContain(
      '改用下载页手动更新',
    )
    // 失败后其余更新操作重新可用
    expect(button(m.container, '立即检查').disabled).toBe(false)
  })

  it('没等到重启：只留收尾文案，绝不留一个会把同一版本重下一遍的按钮', async () => {
    const m = await mount(
      <UpdateCard
        update={withNewer()}
        selfUpdate={fakeSelfUpdate({ phase: 'timeout', version: '0.2.0' })}
      />,
    )
    mounted.push(m.unmount)

    const settled = m.container.querySelector('.self-update-settled')
    expect(settled?.textContent).toBe(
      'v0.2.0 已经装好了，但没等到服务重新启动。请手动重新打开 nagare。',
    )
    // 终态且要用户动手 → 警示框，不是普通结果行，也不是「失败」红字
    expect(settled?.className).toContain('alert-warn')
    expect(settled?.className).not.toContain('result--err')

    // 已经装好了还给「立即更新」= 界面自相矛盾，点一下就重跑整个下载
    expect(maybeButton(m.container, '立即更新')).toBeUndefined()
    expect(maybeButton(m.container, '重试')).toBeUndefined()
    // 「会依次下载 …」那段是在描述按钮要做的事，按钮没了它也不该留着
    expect(m.container.querySelector('.self-update-note')).toBeNull()
  })

  it('更新完成：提示已更新并说明正在刷新页面，同样不留更新按钮', async () => {
    const m = await mount(
      <UpdateCard
        update={withNewer()}
        selfUpdate={fakeSelfUpdate({ phase: 'done', version: '0.2.0' })}
      />,
    )
    mounted.push(m.unmount)
    const status = m.container.querySelector('.self-update-status')
    expect(status?.textContent).toBe('已更新到 v0.2.0，正在刷新页面 …')
    expect(status?.className).toContain('result--ok')
    // 刷新前的一两秒里按钮如果还在，点下去就是把刚装好的版本再装一遍
    expect(maybeButton(m.container, '立即更新')).toBeUndefined()
  })
})
