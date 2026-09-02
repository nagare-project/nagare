// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mountHook } from '../test/harness'

const endpoints = vi.hoisted(() => ({
  fetchSources: vi.fn(),
  setSourceEnabled: vi.fn(),
  reloadSources: vi.fn(),
  selfCheckSource: vi.fn(),
  syncSources: vi.fn(),
  updateRulesConfig: vi.fn(),
}))
vi.mock('../lib/endpoints', () => endpoints)

import { REFRESH_FAILED_MESSAGE, useSources } from './useSources'

const emptyData = {
  sources: [],
  rules: { remoteUrl: '', localDir: '', dir: '', loaded: 0, errors: [], lastLoadedAt: null, lastSyncAt: null },
}

afterEach(() => {
  vi.resetAllMocks()
})

describe('useSources', () => {
  it('变更成功但列表刷新失败时，动作必须失败而不是假报成功', async () => {
    endpoints.fetchSources.mockResolvedValueOnce(emptyData).mockRejectedValueOnce(new Error('后端挂了'))
    endpoints.setSourceEnabled.mockResolvedValue(undefined)
    const h = await mountHook(() => useSources())
    expect(h.result.current.state.phase).toBe('ready')

    let caught: unknown
    await act(async () => {
      try {
        await h.result.current.setEnabled('x', false)
      } catch (err) {
        caught = err
      }
    })
    expect(caught).toBeInstanceOf(Error)
    expect((caught as Error).message).toBe(REFRESH_FAILED_MESSAGE)
    expect(h.result.current.state.phase).toBe('error')
    await h.unmount()
  })

  it('变更成功且刷新成功时动作正常返回', async () => {
    endpoints.fetchSources.mockResolvedValue(emptyData)
    endpoints.reloadSources.mockResolvedValue({ loaded: 2, errors: [] })
    const h = await mountHook(() => useSources())
    let result: unknown
    await act(async () => {
      result = await h.result.current.reloadRules()
    })
    expect(result).toEqual({ loaded: 2, errors: [] })
    expect(h.result.current.state.phase).toBe('ready')
    await h.unmount()
  })
})
