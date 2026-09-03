// @vitest-environment jsdom
// 轮询 + 阻塞请求 + 取消，用 jsdom + fake timers 测状态机与定时器清理。
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { TorrentPlayData, TorrentPlayRequest, TorrentStatus } from '../lib/endpoints'
import { mountHook } from '../test/harness'
import { useTorrentPlay } from './useTorrentPlay'

const MAGNET = 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567'
const TITLE = '[Sakurato] Sousou no Frieren [01][1080p]'
const INTERVAL = 1000

function makeStatus(overrides: Partial<TorrentStatus> = {}): TorrentStatus {
  return {
    active: true,
    phase: 'metadata',
    peers: 3,
    seeders: 1,
    downRate: 1024,
    upRate: 0,
    buffered: 0,
    progress: 0,
    cacheBytes: 0,
    seeding: false,
    ...overrides,
  }
}

const PLAYING: TorrentPlayData = {
  needSelection: false,
  title: 'Frieren - 01',
  danmaku: { state: 'ok', count: 3210 },
}

const NEEDS_SELECTION: TorrentPlayData = {
  needSelection: true,
  files: [
    { index: 0, name: 'ep01.mkv', path: 'ep01.mkv', sizeBytes: 1024, episode: 1 },
    { index: 1, name: 'ep02.mkv', path: 'ep02.mkv', sizeBytes: 1024, episode: 2 },
  ],
}

/** 一组可控的注入函数 */
function makeDeps() {
  return {
    start: vi.fn<(request: TorrentPlayRequest, signal?: AbortSignal) => Promise<TorrentPlayData>>(),
    poll: vi.fn<() => Promise<TorrentStatus>>().mockResolvedValue(makeStatus()),
    stop: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
  }
}

async function mountPlay(deps: ReturnType<typeof makeDeps>) {
  return mountHook(() => useTorrentPlay({ ...deps, intervalMs: INTERVAL }))
}

/** 推进假时钟并让期间触发的异步 setState 落定 */
async function advance(ms: number): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('useTorrentPlay · 正常路径', () => {
  it('play → starting（立刻轮询一次并按间隔续拍）→ streaming', async () => {
    const deps = makeDeps()
    let resolvePlay: (data: TorrentPlayData) => void = () => {}
    deps.start.mockReturnValue(
      new Promise<TorrentPlayData>((resolve) => {
        resolvePlay = resolve
      }),
    )
    const hook = await mountPlay(deps)

    expect(hook.result.current.busy).toBe(false)
    expect(deps.poll).not.toHaveBeenCalled()

    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    expect(hook.result.current.state).toEqual({ phase: 'starting', magnet: MAGNET, title: TITLE })
    expect(hook.result.current.busy).toBe(true)
    expect(deps.start).toHaveBeenCalledExactlyOnceWith(
      { magnet: MAGNET, title: TITLE },
      expect.any(AbortSignal),
    )
    // 立刻拉了一次，不用干等第一个间隔
    expect(deps.poll).toHaveBeenCalledTimes(1)
    expect(hook.result.current.status).toEqual(makeStatus())

    await advance(INTERVAL * 2)
    expect(deps.poll).toHaveBeenCalledTimes(3)

    await act(async () => {
      resolvePlay(PLAYING)
    })
    expect(hook.result.current.state).toEqual({
      phase: 'streaming',
      magnet: MAGNET,
      title: 'Frieren - 01',
      danmaku: { state: 'ok', count: 3210 },
    })
    // 播放开始后仍在轮询：下载还在继续，速度与分享者数要继续显示
    await advance(INTERVAL)
    expect(deps.poll).toHaveBeenCalledTimes(4)

    await hook.unmount()
  })

  it('needSelection → selecting；选定文件后带 fileIndex 重发', async () => {
    const deps = makeDeps()
    deps.start.mockResolvedValueOnce(NEEDS_SELECTION).mockResolvedValueOnce(PLAYING)
    const hook = await mountPlay(deps)

    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    expect(hook.result.current.state).toEqual({
      phase: 'selecting',
      magnet: MAGNET,
      title: TITLE,
      files: NEEDS_SELECTION.needSelection ? NEEDS_SELECTION.files : [],
    })

    await act(async () => {
      hook.result.current.selectFile(1)
    })
    expect(deps.start).toHaveBeenNthCalledWith(
      2,
      { magnet: MAGNET, title: TITLE, fileIndex: 1 },
      expect.any(AbortSignal),
    )
    expect(hook.result.current.state.phase).toBe('streaming')

    await hook.unmount()
  })

  it('播放中后端说会话没了 → 回到空闲并停表', async () => {
    const deps = makeDeps()
    deps.start.mockResolvedValue(PLAYING)
    const hook = await mountPlay(deps)
    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    expect(hook.result.current.state.phase).toBe('streaming')

    deps.poll.mockResolvedValue(makeStatus({ active: false }))
    await advance(INTERVAL)
    expect(hook.result.current.state).toEqual({ phase: 'idle' })
    expect(hook.result.current.status).toBe(null)

    const calls = deps.poll.mock.calls.length
    await advance(INTERVAL * 5)
    expect(deps.poll).toHaveBeenCalledTimes(calls) // 已停表

    await hook.unmount()
  })

  it('starting 期间后端还没建好会话（active=false）不会把界面收掉', async () => {
    const deps = makeDeps()
    deps.poll.mockResolvedValue(makeStatus({ active: false, phase: 'idle' }))
    deps.start.mockReturnValue(new Promise<TorrentPlayData>(() => {})) // 永远不返回
    const hook = await mountPlay(deps)

    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    await advance(INTERVAL * 3)
    expect(hook.result.current.state.phase).toBe('starting')

    await hook.unmount()
  })
})

describe('useTorrentPlay · 分享者为 0 的计时', () => {
  it('连续 0 个 peer 时秒数累加，一有 peer 立刻归零', async () => {
    const deps = makeDeps()
    deps.poll.mockResolvedValue(makeStatus({ peers: 0, seeders: 0 }))
    deps.start.mockReturnValue(new Promise<TorrentPlayData>(() => {}))
    const hook = await mountPlay(deps)

    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    expect(hook.result.current.zeroPeerSeconds).toBe(1) // 首拍就是 0 peer

    await advance(INTERVAL * 4)
    expect(hook.result.current.zeroPeerSeconds).toBe(5)

    deps.poll.mockResolvedValue(makeStatus({ peers: 2 }))
    await advance(INTERVAL)
    expect(hook.result.current.zeroPeerSeconds).toBe(0)

    await hook.unmount()
  })
})

describe('useTorrentPlay · 失败与取消', () => {
  it('play 失败 → error 带后端中文文案；retry 重发同一条磁力', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const deps = makeDeps()
    deps.start
      .mockRejectedValueOnce(new Error('找不到可用的分享者，换一条资源试试'))
      .mockResolvedValueOnce(PLAYING)
    const hook = await mountPlay(deps)

    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    expect(hook.result.current.state).toEqual({
      phase: 'error',
      magnet: MAGNET,
      title: TITLE,
      message: '找不到可用的分享者，换一条资源试试',
    })
    // 失败后不再占着后端，别的行可以播
    expect(hook.result.current.busy).toBe(false)

    await act(async () => {
      hook.result.current.retry()
    })
    expect(deps.start).toHaveBeenNthCalledWith(2, { magnet: MAGNET, title: TITLE }, expect.any(AbortSignal))
    expect(hook.result.current.state.phase).toBe('streaming')

    await hook.unmount()
  })

  it('cancel：立刻回空闲、abort 掉在途长请求、并 POST stop 释放种子', async () => {
    const deps = makeDeps()
    let signal: AbortSignal | undefined
    deps.start.mockImplementation((_request, incoming) => {
      signal = incoming
      return new Promise<TorrentPlayData>(() => {})
    })
    const hook = await mountPlay(deps)

    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    expect(signal?.aborted).toBe(false)

    await act(async () => {
      await hook.result.current.cancel()
    })
    expect(signal?.aborted).toBe(true)
    expect(deps.stop).toHaveBeenCalledTimes(1)
    expect(hook.result.current.state).toEqual({ phase: 'idle' })
    expect(hook.result.current.status).toBe(null)

    // 停表：取消后不该再有轮询
    const calls = deps.poll.mock.calls.length
    await advance(INTERVAL * 3)
    expect(deps.poll).toHaveBeenCalledTimes(calls)

    await hook.unmount()
  })

  it('被取消的请求即使随后失败也不弹错误（abort 不是故障）', async () => {
    const deps = makeDeps()
    let rejectPlay: (err: Error) => void = () => {}
    deps.start.mockReturnValue(
      new Promise<TorrentPlayData>((_resolve, reject) => {
        rejectPlay = reject
      }),
    )
    const hook = await mountPlay(deps)
    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    await act(async () => {
      await hook.result.current.cancel()
    })
    await act(async () => {
      rejectPlay(new Error('The operation was aborted'))
    })
    expect(hook.result.current.state).toEqual({ phase: 'idle' })

    await hook.unmount()
  })

  it('空闲时 cancel 不发 stop（没什么可释放的）', async () => {
    const deps = makeDeps()
    const hook = await mountPlay(deps)
    await act(async () => {
      await hook.result.current.cancel()
    })
    expect(deps.stop).not.toHaveBeenCalled()
    await hook.unmount()
  })

  it('stop 失败时把错误抛给调用方（页面据此提示，不静默）', async () => {
    const deps = makeDeps()
    deps.start.mockReturnValue(new Promise<TorrentPlayData>(() => {}))
    deps.stop.mockRejectedValue(new Error('停止失败'))
    const hook = await mountPlay(deps)
    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })

    await act(async () => {
      await expect(hook.result.current.cancel()).rejects.toThrow('停止失败')
    })
    // 前端确实已经不等了，界面回空闲
    expect(hook.result.current.state).toEqual({ phase: 'idle' })

    await hook.unmount()
  })

  it('轮询失败只把状态置未知，不打断播放流程', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const deps = makeDeps()
    deps.start.mockReturnValue(new Promise<TorrentPlayData>(() => {}))
    deps.poll.mockRejectedValue(new Error('无法连接到 nagare 后端，请确认本地服务已启动'))
    const hook = await mountPlay(deps)

    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    expect(hook.result.current.status).toBe(null)
    expect(hook.result.current.state.phase).toBe('starting')

    // 后端恢复后自己接上，不需要用户重来
    deps.poll.mockResolvedValue(makeStatus())
    await advance(INTERVAL)
    expect(hook.result.current.status).toEqual(makeStatus())

    await hook.unmount()
  })
})

describe('useTorrentPlay · 清理', () => {
  it('卸载后停掉轮询并 abort 在途请求，不留泄漏的 interval', async () => {
    const deps = makeDeps()
    let signal: AbortSignal | undefined
    deps.start.mockImplementation((_request, incoming) => {
      signal = incoming
      return new Promise<TorrentPlayData>(() => {})
    })
    const hook = await mountPlay(deps)
    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    const calls = deps.poll.mock.calls.length

    await hook.unmount()

    expect(signal?.aborted).toBe(true)
    await advance(INTERVAL * 5)
    expect(deps.poll).toHaveBeenCalledTimes(calls)
  })
})

describe('useTorrentPlay · 跨会话竞态', () => {
  /** 一个可以由测试决定何时兑现的 promise */
  function deferred<T>() {
    let resolve!: (value: T) => void
    const promise = new Promise<T>((r) => {
      resolve = r
    })
    return { promise, resolve }
  }

  const MAGNET_B = 'magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98'

  // 回归：取消 A 之后立刻播 B，A 那次还挂在网络上的轮询回来时带着 active=false。
  // 没有代次守卫的话，收摊判定只比对 phase 字符串（此刻正是 B 的 'streaming'），
  // 于是 mpv 还在播 B、界面却把状态条收了、所有播放按钮放开。
  it('旧会话在途的轮询回来时不能把新会话收摊', async () => {
    const deps = makeDeps()
    deps.start.mockResolvedValue(PLAYING)
    const stale = deferred<TorrentStatus>()
    deps.poll.mockReturnValueOnce(stale.promise) // A 的第一次轮询永远挂着
    const hook = await mountPlay(deps)

    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    await advance(0)
    expect(hook.result.current.state.phase).toBe('streaming')

    await act(async () => {
      await hook.result.current.cancel()
    })
    await act(async () => {
      hook.result.current.play(MAGNET_B, TITLE)
    })
    await advance(0)
    expect(hook.result.current.state).toMatchObject({ phase: 'streaming', magnet: MAGNET_B })

    // A 的轮询这时才回来
    await act(async () => {
      stale.resolve(makeStatus({ active: false, peers: 0 }))
    })
    expect(hook.result.current.state).toMatchObject({ phase: 'streaming', magnet: MAGNET_B })

    await hook.unmount()
  })

  // 旧会话的轮询也不该污染新会话的状态条数据（速度、分享者数、零分享者计时）。
  it('旧会话的轮询数据不覆盖新会话的状态', async () => {
    const deps = makeDeps()
    deps.start.mockResolvedValue(PLAYING)
    const stale = deferred<TorrentStatus>()
    deps.poll.mockReturnValueOnce(stale.promise)
    const hook = await mountPlay(deps)

    await act(async () => {
      hook.result.current.play(MAGNET, TITLE)
    })
    await advance(0)
    await act(async () => {
      await hook.result.current.cancel()
    })
    await act(async () => {
      hook.result.current.play(MAGNET_B, TITLE)
    })
    await advance(0)
    const fresh = hook.result.current.status

    await act(async () => {
      stale.resolve(makeStatus({ active: true, peers: 999, downRate: 1 }))
    })
    expect(hook.result.current.status).toEqual(fresh)

    await hook.unmount()
  })
})
