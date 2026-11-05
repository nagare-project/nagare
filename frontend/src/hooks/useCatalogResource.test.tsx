// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../test/harness'
import { getByRole } from '../test/queries'
import { FreshnessNote } from '../components/catalog/FreshnessNote'
import type { CatalogFreshness } from '../lib/endpoints'
import { STALE_REFETCH_MS, useCatalogResource } from './useCatalogResource'
import type { CatalogResource } from './useCatalogResource'

interface Payload { items: string[]; freshness?: CatalogFreshness }

beforeEach(() => vi.useFakeTimers())
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })
async function flush(ms = 0) { await act(async () => { vi.advanceTimersByTime(ms); await Promise.resolve() }) }

const stale = (): CatalogFreshness => ({ fetchedAt: new Date(Date.now() - 3600_000).toISOString(), stale: true, offline: false })

function Probe({ resourceKey, load, result }: { resourceKey: string; load: (signal: AbortSignal) => Promise<Payload>; result: { current?: CatalogResource<Payload> } }) {
  result.current = useCatalogResource(resourceKey, load, { fallbackError: '读取失败' })
  return <FreshnessNote freshness={result.current.data?.freshness} refreshing={result.current.refreshing} />
}

describe('目录数据的读取', () => {
  it('过期且在线时，等一会儿在后台再取一次，原地换上新数据（不回到 loading），只追问一次', async () => {
    const load = vi.fn<(signal: AbortSignal) => Promise<Payload>>()
      .mockResolvedValueOnce({ items: ['旧'], freshness: stale() })
      .mockResolvedValueOnce({ items: ['新'] })
    const result: { current?: CatalogResource<Payload> } = {}
    const { container, unmount } = await mount(<Probe resourceKey="discover" load={load} result={result} />)
    await flush()
    expect(result.current?.data?.items).toEqual(['旧'])
    expect(result.current?.refreshing).toBe(true)
    expect(container.textContent).toBe('更新于 1 小时前 · 正在刷新')
    const height = container.querySelector('p')
    await flush(STALE_REFETCH_MS)
    expect(load).toHaveBeenCalledTimes(2)
    expect(result.current?.loading).toBe(false)
    expect(result.current?.data?.items).toEqual(['新'])
    // 细条让出的位置留着（不跳版），只是不再念、也看不见
    expect(container.querySelector('p')).toBe(height)
    expect(container.querySelector('p')?.getAttribute('aria-hidden')).toBe('true')
    expect(container.textContent).toBe('')
    await flush(STALE_REFETCH_MS * 3)
    expect(load).toHaveBeenCalledTimes(2)
    await unmount()
  })

  it('追问回来仍是过期的：照实说「更新于 …」，不再说正在刷新，也不轮询', async () => {
    const load = vi.fn(async () => ({ items: ['旧'], freshness: stale() }))
    const result: { current?: CatalogResource<Payload> } = {}
    const { container, unmount } = await mount(<Probe resourceKey="discover" load={load} result={result} />)
    await flush()
    await flush(STALE_REFETCH_MS)
    expect(container.textContent).toBe('更新于 1 小时前')
    await flush(STALE_REFETCH_MS * 3)
    expect(load).toHaveBeenCalledTimes(2)
    await unmount()
  })

  it('离线的一份不追问（后端刚刷新失败，一分钟内不会再试）', async () => {
    const load = vi.fn(async () => ({ items: ['旧'], freshness: { ...stale(), offline: true } }))
    const result: { current?: CatalogResource<Payload> } = {}
    const { container, unmount } = await mount(<Probe resourceKey="discover" load={load} result={result} />)
    await flush()
    // 读屏在进入离线时听到一次
    expect(getByRole(container, 'status').textContent).toBe('离线 · 更新于 1 小时前')
    await flush(STALE_REFETCH_MS * 2)
    expect(load).toHaveBeenCalledTimes(1)
    // 看得见的细条一直在；播报念过之后已经清空
    expect(container.querySelector('p')?.textContent).toBe('离线 · 更新于 1 小时前')
    expect(getByRole(container, 'status').textContent).toBe('')
    await unmount()
  })

  it('后台那次失败：留着已经显示的内容，不换成错误', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const load = vi.fn<(signal: AbortSignal) => Promise<Payload>>()
      .mockResolvedValueOnce({ items: ['旧'], freshness: stale() })
      .mockRejectedValueOnce(new Error('断了'))
    const result: { current?: CatalogResource<Payload> } = {}
    const { unmount } = await mount(<Probe resourceKey="discover" load={load} result={result} />)
    await flush()
    await flush(STALE_REFETCH_MS)
    expect(result.current?.error).toBeUndefined()
    expect(result.current?.data?.items).toEqual(['旧'])
    expect(result.current?.refreshing).toBe(false)
    await unmount()
  })

  it('首次读取失败给错误；重试再读；换了 key 先清掉旧数据（回到 loading）再读', async () => {
    let finishWinter!: (payload: Payload) => void
    const load = vi.fn<(signal: AbortSignal) => Promise<Payload>>()
      .mockRejectedValueOnce(new Error('连不上'))
      .mockResolvedValueOnce({ items: ['秋'] })
      .mockImplementationOnce(() => new Promise((resolve) => { finishWinter = resolve }))
    const result: { current?: CatalogResource<Payload> } = {}
    const view = await mount(<Probe resourceKey="FALL-2026" load={load} result={result} />)
    await flush()
    expect(result.current?.error).toBe('连不上')
    await act(async () => result.current!.retry())
    await flush()
    expect(result.current?.error).toBeUndefined()
    expect(result.current?.data?.items).toEqual(['秋'])
    await view.rerender(<Probe resourceKey="WINTER-2027" load={load} result={result} />)
    expect(result.current?.data).toBeUndefined()
    expect(result.current?.loading).toBe(true)
    await act(async () => finishWinter({ items: ['冬'] }))
    expect(result.current?.data?.items).toEqual(['冬'])
    await view.unmount()
  })
})
