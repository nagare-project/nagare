// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MpvInfo } from '../../lib/endpoints'
import { mount } from '../../test/harness'

const endpoints = vi.hoisted(() => ({ redetectMpv: vi.fn() }))
vi.mock('../../lib/endpoints', () => endpoints)

import { MpvCard } from './MpvCard'

const MISSING: MpvInfo = {
  found: false,
  hint: '未检测到 mpv，macOS 需要自行安装。',
  install: {
    command: 'brew install mpv',
    url: 'https://mpv.io/installation/',
    note: '用 Homebrew 安装最省事；装完回来点「重新检测」。',
  },
}

const FOUND: MpvInfo = {
  found: true,
  path: '/opt/homebrew/bin/mpv',
  version: '0.38.0',
  source: 'path',
}

function stubClipboard(writeText: (text: string) => Promise<void>): void {
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
}

function button(container: HTMLElement, text: string): HTMLButtonElement {
  const found = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === text)
  if (found === undefined) throw new Error(`找不到按钮「${text}」`)
  return found
}

const mounted: Array<() => Promise<void>> = []

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.useRealTimers()
  vi.resetAllMocks()
  vi.restoreAllMocks()
  Reflect.deleteProperty(navigator, 'clipboard')
})

describe('MpvCard · 未找到', () => {
  it('显示未找到 + hint + 命令块 + 说明 + 外链（新标签页、noopener）', async () => {
    const m = await mount(<MpvCard mpv={MISSING} onReload={vi.fn()} />)
    mounted.push(m.unmount)
    const { container } = m
    expect(container.querySelector('.kv-list dd')?.textContent).toBe('未找到')
    expect(container.querySelector('.alert-warn')?.textContent).toBe(MISSING.hint)
    expect(container.querySelector('.mpv-command-text')?.textContent).toBe('brew install mpv')
    expect(container.querySelector('.mpv-install-note')?.textContent).toContain('Homebrew')
    const a = container.querySelector('a.mpv-install-link')
    expect(a?.getAttribute('href')).toBe('https://mpv.io/installation/')
    expect(a?.getAttribute('target')).toBe('_blank')
    expect(a?.getAttribute('rel')).toContain('noopener')
    expect(a?.getAttribute('rel')).toContain('noreferrer')
    expect(container.textContent).toContain('无需重启')
  })

  it('windows：command 为空串时不出命令块，只留说明与外链', async () => {
    const m = await mount(
      <MpvCard
        mpv={{ ...MISSING, install: { ...MISSING.install!, command: '' } }}
        onReload={vi.fn()}
      />,
    )
    mounted.push(m.unmount)
    expect(m.container.querySelector('.mpv-command')).toBeNull()
    expect(m.container.querySelector('a.mpv-install-link')).not.toBeNull()
  })

  it('非 http(s) 的 url 不渲染成链接', async () => {
    const m = await mount(
      <MpvCard
        mpv={{ ...MISSING, install: { ...MISSING.install!, url: 'javascript:alert(1)' } }}
        onReload={vi.fn()}
      />,
    )
    mounted.push(m.unmount)
    expect(m.container.querySelector('a')).toBeNull()
  })

  it('「复制」写入命令，显示「已复制」2 秒后复原', async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>().mockResolvedValue(undefined)
    stubClipboard(writeText)
    const m = await mount(<MpvCard mpv={MISSING} onReload={vi.fn()} />)
    mounted.push(m.unmount)
    const copy = button(m.container, '复制')
    await act(async () => {
      copy.click()
    })
    expect(writeText).toHaveBeenCalledExactlyOnceWith('brew install mpv')
    expect(copy.textContent).toBe('已复制 ✓')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000)
    })
    expect(copy.textContent).toBe('复制')
  })

  it('剪贴板与回退都失败时显示「复制失败」', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const m = await mount(<MpvCard mpv={MISSING} onReload={vi.fn()} />)
    mounted.push(m.unmount)
    const copy = button(m.container, '复制')
    await act(async () => {
      copy.click()
    })
    expect(copy.textContent).toBe('复制失败')
    expect(copy.classList.contains('mpv-copy--failed')).toBe(true)
  })

  it('「重新检测」成功：调接口 → 刷新设置 → 显示找到的版本', async () => {
    endpoints.redetectMpv.mockResolvedValue(FOUND)
    const onReload = vi.fn().mockResolvedValue(undefined)
    const m = await mount(<MpvCard mpv={MISSING} onReload={onReload} />)
    mounted.push(m.unmount)
    await act(async () => {
      button(m.container, '重新检测').click()
    })
    expect(endpoints.redetectMpv).toHaveBeenCalledTimes(1)
    expect(onReload).toHaveBeenCalledTimes(1)
    // 注意别选到复制按钮旁那个视觉隐藏的 live region（DOM 顺序在结果行之前）
    expect(m.container.querySelector('.result--ok[role="status"]')?.textContent).toBe(
      '已找到 mpv 0.38.0',
    )
  })

  it('「重新检测」仍未找到：给出明确提示', async () => {
    endpoints.redetectMpv.mockResolvedValue({ found: false, hint: MISSING.hint })
    const m = await mount(<MpvCard mpv={MISSING} onReload={vi.fn().mockResolvedValue(undefined)} />)
    mounted.push(m.unmount)
    await act(async () => {
      button(m.container, '重新检测').click()
    })
    expect(m.container.querySelector('.result--err[role="status"]')?.textContent).toContain(
      '仍未找到 mpv',
    )
  })

  it('「重新检测」失败：显示中文错误，不刷新设置', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    endpoints.redetectMpv.mockRejectedValue(new Error('无法连接到 nagare 后端，请确认本地服务已启动'))
    const onReload = vi.fn()
    const m = await mount(<MpvCard mpv={MISSING} onReload={onReload} />)
    mounted.push(m.unmount)
    await act(async () => {
      button(m.container, '重新检测').click()
    })
    expect(onReload).not.toHaveBeenCalled()
    expect(m.container.querySelector('.result--err[role="status"]')?.textContent).toBe(
      '无法连接到 nagare 后端，请确认本地服务已启动',
    )
  })
})

describe('MpvCard · 已找到', () => {
  it('显示版本 / 路径 / 来源中文，没有安装指引', async () => {
    const m = await mount(<MpvCard mpv={FOUND} onReload={vi.fn()} />)
    mounted.push(m.unmount)
    const text = m.container.textContent ?? ''
    expect(text).toContain('已找到 ✓')
    expect(text).toContain('0.38.0')
    expect(text).toContain('/opt/homebrew/bin/mpv')
    expect(text).toContain('PATH 环境变量')
    expect(m.container.querySelector('.mpv-install')).toBeNull()
    expect(m.container.querySelector('.alert-warn')).toBeNull()
  })

  it.each<[MpvInfo['source'], string]>([
    ['explicit', '显式路径'],
    ['bundled', '内置'],
    ['known', '常见安装位置'],
  ])('source=%s → %s', async (source, expected) => {
    const m = await mount(<MpvCard mpv={{ ...FOUND, source }} onReload={vi.fn()} />)
    mounted.push(m.unmount)
    expect(m.container.textContent).toContain(expected)
  })
})
