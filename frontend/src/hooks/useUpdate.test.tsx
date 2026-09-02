// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { UpdateView } from '../lib/endpoints'
import { ApiAuthError, ApiError } from '../lib/api'
import { mountHook } from '../test/harness'

const endpoints = vi.hoisted(() => ({
  fetchUpdate: vi.fn(),
  checkUpdate: vi.fn(),
  setUpdateEnabled: vi.fn(),
}))
vi.mock('../lib/endpoints', () => endpoints)

import { useUpdate } from './useUpdate'

const IDLE: UpdateView = {
  enabled: true,
  current: '0.1.0',
  latest: '',
  available: false,
  url: '',
  checkedAt: null,
  error: '',
}

const NEWER: UpdateView = {
  ...IDLE,
  latest: 'v0.2.0',
  available: true,
  url: 'https://github.com/nagare-project/nagare/releases/tag/v0.2.0',
  checkedAt: 1_756_800_000,
}

afterEach(() => {
  vi.resetAllMocks()
  vi.restoreAllMocks()
})

describe('useUpdate', () => {
  it('挂载即拉取 GET /api/update，成功进 ready', async () => {
    endpoints.fetchUpdate.mockResolvedValue(IDLE)
    const h = await mountHook(() => useUpdate())
    expect(endpoints.fetchUpdate).toHaveBeenCalled()
    expect(h.result.current.state).toEqual({ phase: 'ready', data: IDLE })
    await h.unmount()
  })

  it('401 → unauthorized；其余失败 → error 并带中文文案', async () => {
    endpoints.fetchUpdate.mockRejectedValueOnce(new ApiAuthError('缺少有效 token'))
    const h1 = await mountHook(() => useUpdate())
    expect(h1.result.current.state.phase).toBe('unauthorized')
    await h1.unmount()

    vi.spyOn(console, 'error').mockImplementation(() => {})
    endpoints.fetchUpdate.mockRejectedValueOnce(new ApiError('后端挂了', 500))
    const h2 = await mountHook(() => useUpdate())
    expect(h2.result.current.state).toEqual({ phase: 'error', message: '后端挂了' })
    await h2.unmount()
  })

  it('check() 用返回值刷新状态机并把它交给调用方', async () => {
    endpoints.fetchUpdate.mockResolvedValue(IDLE)
    endpoints.checkUpdate.mockResolvedValue(NEWER)
    const h = await mountHook(() => useUpdate())

    let result: UpdateView | undefined
    await act(async () => {
      result = await h.result.current.check()
    })
    expect(result).toEqual(NEWER)
    expect(h.result.current.state).toEqual({ phase: 'ready', data: NEWER })
    await h.unmount()
  })

  it('setEnabled() 同步返回值；动作失败原样抛出且状态机不动', async () => {
    endpoints.fetchUpdate.mockResolvedValue(IDLE)
    endpoints.setUpdateEnabled
      .mockResolvedValueOnce({ ...IDLE, enabled: false })
      .mockRejectedValueOnce(new ApiError('写配置失败', 500))
    const h = await mountHook(() => useUpdate())

    await act(async () => {
      await h.result.current.setEnabled(false)
    })
    expect(endpoints.setUpdateEnabled).toHaveBeenLastCalledWith(false)
    expect(h.result.current.state).toEqual({ phase: 'ready', data: { ...IDLE, enabled: false } })

    let caught: unknown
    await act(async () => {
      try {
        await h.result.current.setEnabled(true)
      } catch (err) {
        caught = err
      }
    })
    expect(caught).toBeInstanceOf(ApiError)
    expect(h.result.current.state).toEqual({ phase: 'ready', data: { ...IDLE, enabled: false } })
    await h.unmount()
  })
})
