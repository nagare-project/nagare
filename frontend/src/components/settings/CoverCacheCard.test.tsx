// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'

const endpoints = vi.hoisted(() => ({ clearCoverCache: vi.fn() }))
vi.mock('../../lib/endpoints', () => endpoints)

import { CoverCacheCard, clearedText } from './CoverCacheCard'

const CACHE = { bytes: 120 * 1024 * 1024, count: 640, maxBytes: 300 * 1024 * 1024 }

function button(container: HTMLElement, text: string): HTMLButtonElement | undefined {
  return Array.from(container.querySelectorAll('button')).find((b) => b.textContent === text)
}

function status(container: HTMLElement): HTMLElement {
  return container.querySelector('[role="status"]') as HTMLElement
}

const mounted: Array<() => Promise<void>> = []
afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.resetAllMocks()
  vi.restoreAllMocks()
})

async function render(onReload = vi.fn().mockResolvedValue(undefined)) {
  const m = await mount(<CoverCacheCard cache={CACHE} onReload={onReload} />)
  mounted.push(m.unmount)
  return { ...m, onReload }
}

describe('CoverCacheCard', () => {
  it('显示占用、张数与上限；首次渲染不抢焦点', async () => {
    const { container } = await render()
    expect(container.querySelector('h2')?.textContent).toBe('封面缓存')
    expect(container.textContent).toContain('120 MB · 640 张')
    expect(container.textContent).toContain('300 MB')
    expect(document.activeElement).toBe(document.body)
    expect(status(container).textContent).toBe('')
  })

  it('点「清空封面缓存」只弹内联确认、焦点落在「取消」；取消后焦点回到按钮，不调接口', async () => {
    const { container } = await render()
    await act(async () => {
      button(container, '清空封面缓存')?.click()
    })
    expect(container.querySelector('[role="group"]')?.getAttribute('aria-label')).toBe('确认清空封面缓存')
    expect(document.activeElement).toBe(button(container, '取消'))
    expect(endpoints.clearCoverCache).not.toHaveBeenCalled()

    await act(async () => {
      button(container, '取消')?.click()
    })
    expect(container.querySelector('[role="group"]')).toBeNull()
    expect(document.activeElement).toBe(button(container, '清空封面缓存'))
    expect(endpoints.clearCoverCache).not.toHaveBeenCalled()
  })

  it('确认后清空、报告新占用并刷新设置', async () => {
    endpoints.clearCoverCache.mockResolvedValue({ bytes: 0, count: 0, maxBytes: CACHE.maxBytes, skipped: 0 })
    const { container, onReload } = await render()
    await act(async () => {
      button(container, '清空封面缓存')?.click()
    })
    await act(async () => {
      button(container, '确认清空')?.click()
    })
    expect(endpoints.clearCoverCache).toHaveBeenCalledTimes(1)
    expect(onReload).toHaveBeenCalledTimes(1)
    expect(status(container).textContent).toBe('已清空')
    expect(container.querySelector('[role="group"]')).toBeNull()
    expect(document.activeElement).toBe(button(container, '清空封面缓存'))
  })

  it('清空中按钮禁用、状态行念「正在清空」，点了也不会重复提交', async () => {
    let resolve: (v: unknown) => void = () => {}
    endpoints.clearCoverCache.mockReturnValue(new Promise((r) => { resolve = r }))
    const { container } = await render()
    await act(async () => {
      button(container, '清空封面缓存')?.click()
    })
    await act(async () => {
      button(container, '确认清空')?.click()
    })
    const busy = button(container, '正在清空 …')
    expect(busy?.disabled).toBe(true)
    expect(button(container, '取消')?.disabled).toBe(true)
    expect(status(container).textContent).toBe('正在清空 …')
    await act(async () => {
      busy?.click()
    })
    expect(endpoints.clearCoverCache).toHaveBeenCalledTimes(1)

    await act(async () => {
      resolve({ bytes: 2048, count: 1, maxBytes: CACHE.maxBytes, skipped: 1 })
    })
    expect(status(container).textContent).toBe('已清空（1 张正在显示，这次没删，稍后可再清一次）')
    expect(document.activeElement).toBe(button(container, '清空封面缓存'))
  })

  it('刷新设置失败不算清空失败', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    endpoints.clearCoverCache.mockResolvedValue({ bytes: 0, count: 0, maxBytes: CACHE.maxBytes, skipped: 0 })
    const { container } = await render(vi.fn().mockRejectedValue(new Error('offline')))
    await act(async () => {
      button(container, '清空封面缓存')?.click()
    })
    await act(async () => {
      button(container, '确认清空')?.click()
    })
    expect(status(container).textContent).toBe('已清空')
    expect(status(container).className).not.toContain('result--err')
  })

  it('请求期间焦点被挪到卡片外时，完成后不把焦点抢回来', async () => {
    let resolve: (v: unknown) => void = () => {}
    endpoints.clearCoverCache.mockReturnValue(new Promise((r) => { resolve = r }))
    const outside = document.createElement('button')
    document.body.appendChild(outside)
    try {
      const { container } = await render()
      await act(async () => {
        button(container, '清空封面缓存')?.click()
      })
      await act(async () => {
        button(container, '确认清空')?.click()
      })
      outside.focus()
      await act(async () => {
        resolve({ bytes: 0, count: 0, maxBytes: CACHE.maxBytes, skipped: 0 })
      })
      expect(document.activeElement).toBe(outside)
    } finally {
      outside.remove()
    }
  })

  it('失败时红字报错，不刷新设置', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    endpoints.clearCoverCache.mockRejectedValue(new Error('封面缓存不可用'))
    const { container, onReload } = await render()
    await act(async () => {
      button(container, '清空封面缓存')?.click()
    })
    await act(async () => {
      button(container, '确认清空')?.click()
    })
    expect(status(container).className).toContain('result--err')
    expect(status(container).textContent).toBe('清空失败：封面缓存不可用')
    expect(onReload).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(button(container, '清空封面缓存'))
  })

  it('没有跳过的封面时只说「已清空」', () => {
    expect(clearedText({ bytes: 0, count: 0, maxBytes: 1, skipped: 0 })).toBe('已清空')
  })
})
