// @vitest-environment jsdom
// 阻塞请求 + 重启期轮询 + 超时兜底，用 jsdom + fake timers 测状态机与定时器清理。
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { HealthData } from '../lib/api'
import type { ApplyUpdateData } from '../lib/endpoints'
import { mountHook } from '../test/harness'
import { useSelfUpdate } from './useSelfUpdate'

const INTERVAL = 1000
const TIMEOUT = 10_000
const RELOAD_DELAY = 1500
/** 时限内一共问几次 health */
const MAX_ATTEMPTS = TIMEOUT / INTERVAL

const NEW_VERSION = '0.2.0'
const OLD_HEALTH: HealthData = { status: 'ok', version: '0.1.0' }
const NEW_HEALTH: HealthData = { status: 'ok', version: '0.2.0' }

function makeDeps() {
  return {
    apply: vi.fn<() => Promise<ApplyUpdateData>>().mockResolvedValue({ version: NEW_VERSION }),
    health: vi.fn<() => Promise<HealthData>>().mockResolvedValue(OLD_HEALTH),
    reload: vi.fn<() => void>(),
  }
}

async function mountSelfUpdate(deps: ReturnType<typeof makeDeps>) {
  return mountHook(() =>
    useSelfUpdate({
      ...deps,
      pollIntervalMs: INTERVAL,
      restartTimeoutMs: TIMEOUT,
      reloadDelayMs: RELOAD_DELAY,
    }),
  )
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

describe('useSelfUpdate · 正常路径', () => {
  it('start → applying（走秒）→ restarting → health 版本对上 → done → 刷新页面', async () => {
    const deps = makeDeps()
    let finishApply: (data: ApplyUpdateData) => void = () => {}
    deps.apply.mockReturnValue(
      new Promise<ApplyUpdateData>((resolve) => {
        finishApply = resolve
      }),
    )
    const hook = await mountSelfUpdate(deps)

    expect(hook.result.current.state).toEqual({ phase: 'idle' })
    expect(hook.result.current.busy).toBe(false)

    await act(async () => {
      hook.result.current.start()
    })
    expect(deps.apply).toHaveBeenCalledTimes(1)
    expect(hook.result.current.state).toEqual({ phase: 'applying' })
    expect(hook.result.current.busy).toBe(true)

    // 下载期间没有服务端进度，靠走秒表明「还活着」
    expect(hook.result.current.elapsedSec).toBe(0)
    await advance(3 * INTERVAL)
    expect(hook.result.current.elapsedSec).toBe(3)
    // apply 在途时一次 health 都不该发
    expect(deps.health).not.toHaveBeenCalled()

    await act(async () => {
      finishApply({ version: NEW_VERSION })
    })
    expect(hook.result.current.state).toEqual({ phase: 'restarting', version: NEW_VERSION })
    expect(hook.result.current.elapsedSec).toBe(0)

    deps.health.mockResolvedValue(NEW_HEALTH)
    await advance(INTERVAL)
    expect(deps.health).toHaveBeenCalledTimes(1)
    expect(hook.result.current.state).toEqual({ phase: 'done', version: NEW_VERSION })
    expect(hook.result.current.busy).toBe(false)

    // 刷新留一小段时间让用户看清「已更新到 vX」
    expect(deps.reload).not.toHaveBeenCalled()
    await advance(RELOAD_DELAY)
    expect(deps.reload).toHaveBeenCalledTimes(1)

    await hook.unmount()
  })

  it('旧进程还答着旧版本号时继续等，换成新版本才算起来了', async () => {
    const deps = makeDeps()
    const hook = await mountSelfUpdate(deps)
    await act(async () => {
      hook.result.current.start()
    })
    expect(hook.result.current.state.phase).toBe('restarting')

    // 正在退出的旧进程照样答得上 health —— 只看「答不答得上」会提前收工
    await advance(INTERVAL * 3)
    expect(deps.health).toHaveBeenCalledTimes(3)
    expect(hook.result.current.state.phase).toBe('restarting')

    deps.health.mockResolvedValue({ status: 'ok', version: 'v0.2.0' }) // 带 v 前缀也算同一版
    await advance(INTERVAL)
    expect(hook.result.current.state).toEqual({ phase: 'done', version: NEW_VERSION })

    await hook.unmount()
  })

  it('更新中重复 start 不会发第二次 apply', async () => {
    const deps = makeDeps()
    deps.apply.mockReturnValue(new Promise<ApplyUpdateData>(() => {})) // 永远不返回
    const hook = await mountSelfUpdate(deps)

    await act(async () => {
      hook.result.current.start()
    })
    await act(async () => {
      hook.result.current.start()
    })
    expect(deps.apply).toHaveBeenCalledTimes(1)

    await hook.unmount()
  })
})

describe('useSelfUpdate · 重启期的失败是预期内的', () => {
  // 这条是重点：后端重启时连接必然被拒几次，一次都不能当故障弹出去。
  it('health 连续失败不进 error，后端回来后照样走到 done', async () => {
    const deps = makeDeps()
    deps.health.mockRejectedValue(new Error('无法连接到 nagare 后端，请确认本地服务已启动'))
    const hook = await mountSelfUpdate(deps)

    await act(async () => {
      hook.result.current.start()
    })
    expect(hook.result.current.state.phase).toBe('restarting')

    await advance(INTERVAL * 4)
    expect(deps.health).toHaveBeenCalledTimes(4)
    // 全都失败了，但状态机一动不动，也没有错误文案
    expect(hook.result.current.state).toEqual({ phase: 'restarting', version: NEW_VERSION })
    expect(hook.result.current.elapsedSec).toBe(4)

    deps.health.mockResolvedValue(NEW_HEALTH)
    await advance(INTERVAL)
    expect(hook.result.current.state).toEqual({ phase: 'done', version: NEW_VERSION })

    await hook.unmount()
  })

  it('等不到就绪 → timeout（区别于 error：更新其实已经装好了）', async () => {
    const deps = makeDeps()
    deps.health.mockRejectedValue(new Error('无法连接到 nagare 后端，请确认本地服务已启动'))
    const hook = await mountSelfUpdate(deps)

    await act(async () => {
      hook.result.current.start()
    })
    await advance(INTERVAL * (MAX_ATTEMPTS - 1))
    expect(hook.result.current.state.phase).toBe('restarting')

    await advance(INTERVAL)
    expect(hook.result.current.state).toEqual({ phase: 'timeout', version: NEW_VERSION })
    expect(hook.result.current.busy).toBe(false)

    // 超时后停表，也绝不刷新页面（新后端根本没起来，刷了只会白屏）
    const calls = deps.health.mock.calls.length
    await advance(INTERVAL * 5 + RELOAD_DELAY)
    expect(deps.health).toHaveBeenCalledTimes(calls)
    expect(deps.reload).not.toHaveBeenCalled()

    await hook.unmount()
  })
})

describe('useSelfUpdate · 失败与重试', () => {
  it('apply 失败 → error 带后端中文文案；再次 start 重来一轮', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const deps = makeDeps()
    deps.apply
      .mockRejectedValueOnce(new Error('更新包签名校验失败，已丢弃下载的文件'))
      .mockResolvedValueOnce({ version: NEW_VERSION })
    const hook = await mountSelfUpdate(deps)

    await act(async () => {
      hook.result.current.start()
    })
    expect(hook.result.current.state).toEqual({
      phase: 'error',
      message: '更新包签名校验失败，已丢弃下载的文件',
    })
    expect(hook.result.current.busy).toBe(false)
    // 失败后不该有任何轮询在跑
    await advance(INTERVAL * 3)
    expect(deps.health).not.toHaveBeenCalled()

    await act(async () => {
      hook.result.current.start()
    })
    expect(deps.apply).toHaveBeenCalledTimes(2)
    expect(hook.result.current.state).toEqual({ phase: 'restarting', version: NEW_VERSION })

    await hook.unmount()
  })

  it('异常没有可读 message 时回退到中文兜底文案', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const deps = makeDeps()
    deps.apply.mockRejectedValue(new Error(''))
    const hook = await mountSelfUpdate(deps)

    await act(async () => {
      hook.result.current.start()
    })
    expect(hook.result.current.state).toEqual({ phase: 'error', message: '更新失败' })

    await hook.unmount()
  })

  it('重试后，上一轮在途的 apply 回来时不能再动状态机', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const deps = makeDeps()
    let failFirst: (err: Error) => void = () => {}
    deps.apply
      .mockReturnValueOnce(
        new Promise<ApplyUpdateData>((_resolve, reject) => {
          failFirst = reject
        }),
      )
      .mockResolvedValueOnce({ version: NEW_VERSION })
    const hook = await mountSelfUpdate(deps)

    await act(async () => {
      hook.result.current.start()
    })
    // 第一轮还挂着的时候不可能再点（按钮禁用），这里直接模拟状态机层的竞态：
    // 先让它失败进 error，再重试，然后让第一轮那个 promise 又 reject 一次。
    await act(async () => {
      failFirst(new Error('第一轮失败'))
    })
    await act(async () => {
      hook.result.current.start()
    })
    expect(hook.result.current.state.phase).toBe('restarting')

    await act(async () => {
      failFirst(new Error('第一轮的迟到错误'))
    })
    expect(hook.result.current.state).toEqual({ phase: 'restarting', version: NEW_VERSION })

    await hook.unmount()
  })
})

describe('useSelfUpdate · 清理', () => {
  it('卸载后不再轮询、也不再刷新页面', async () => {
    const deps = makeDeps()
    const hook = await mountSelfUpdate(deps)
    await act(async () => {
      hook.result.current.start()
    })
    await advance(INTERVAL * 2)
    const calls = deps.health.mock.calls.length
    expect(calls).toBeGreaterThan(0)

    await hook.unmount()

    await advance(INTERVAL * 10)
    expect(deps.health).toHaveBeenCalledTimes(calls)
    expect(deps.reload).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('done 之后立刻卸载 → 不会对着已经不在的页面执行刷新', async () => {
    const deps = makeDeps()
    deps.health.mockResolvedValue(NEW_HEALTH)
    const hook = await mountSelfUpdate(deps)
    await act(async () => {
      hook.result.current.start()
    })
    await advance(INTERVAL)
    expect(hook.result.current.state.phase).toBe('done')

    await hook.unmount()
    await advance(RELOAD_DELAY * 2)
    expect(deps.reload).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })
})
