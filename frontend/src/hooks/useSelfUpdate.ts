import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { fetchHealth } from '../lib/api'
import type { HealthData } from '../lib/api'
import { applyUpdate } from '../lib/endpoints'
import type { ApplyUpdateData } from '../lib/endpoints'
import { errorText, normalizeVersion } from '../lib/format'

/** 等后端重启时多久问一次 /api/health */
export const HEALTH_POLL_INTERVAL_MS = 1000

/** 等后端重启的总时限；超时不是失败，只是「没等到」，要用另一套措辞 */
export const RESTART_TIMEOUT_MS = 60_000

/** 已用秒数的走表间隔（界面据此给出「还活着」的反馈） */
export const ELAPSED_TICK_MS = 1000

/** done 之后停留多久再刷新页面，让用户看清「已更新到 vX」 */
export const RELOAD_DELAY_MS = 1500

/**
 * 一键更新的状态机。
 *
 * idle → applying →（后端答复新版本号）restarting →（health 版本对上）done → 刷新页面
 *                                                 └（等到超时）timeout
 *                 └（请求失败）error
 *
 * timeout 与 error 是**两件事**：前者更新已经装好了，只是没等到服务回来，
 * 用户手动重开就好；后者是没装成，用户可以重试或改用下载页。措辞不能混。
 */
export type SelfUpdateState =
  | { phase: 'idle' }
  | { phase: 'applying' }
  | { phase: 'restarting'; version: string }
  | { phase: 'done'; version: string }
  | { phase: 'timeout'; version: string }
  | { phase: 'error'; message: string }

export interface UseSelfUpdateOptions {
  /** 可注入的请求函数（测试替身用）；默认走真实 API */
  apply?: () => Promise<ApplyUpdateData>
  health?: () => Promise<HealthData>
  /** 刷新页面。默认 window.location.reload()，测试必须注入替身 */
  reload?: () => void
  pollIntervalMs?: number
  restartTimeoutMs?: number
  reloadDelayMs?: number
}

export interface UseSelfUpdateResult {
  state: SelfUpdateState
  /** 当前阶段已经过去多少秒；applying / restarting 之外恒为 0 */
  elapsedSec: number
  /** 更新占着后端的两个阶段 —— 其余更新相关操作据此一并禁用 */
  busy: boolean
  /** 发起一键更新；已经在更新中时忽略（重复点不会发第二次 apply） */
  start: () => void
}

/** 占着后端的两个阶段 */
function isBusyPhase(phase: SelfUpdateState['phase']): boolean {
  return phase === 'applying' || phase === 'restarting'
}

/**
 * 新版本已经落盘的两个终态：done（等着刷新）与 timeout（等用户手动重开）。
 *
 * 这两个状态下**绝不能**再给「立即更新」按钮：文案说的是「已经装好了」，
 * 按钮却会把同一个版本重新下载安装一遍 —— 界面自相矛盾时用户会照着按钮点，
 * 于是白白重跑一次几十上百 MB 的下载与替换。
 */
export function isSettledPhase(phase: SelfUpdateState['phase']): boolean {
  return phase === 'done' || phase === 'timeout'
}

/**
 * 一键更新编排 hook（M4 阶段 B）。
 *
 * POST /api/update/apply 是阻塞请求（下载 20–120MB + 验签 + 解包，几分钟都正常），
 * 返回后后端约一秒内重启自己 —— 于是这里分成两段等：
 * 1. applying：请求在途，界面靠走秒表明「还在下，没卡死」；
 * 2. restarting：1 秒一次问 /api/health，等它带着**新版本号**回来。
 *
 * 挂在 RootLayout 上（经 SelfUpdateContext 共享给顶部提示条与设置页的更新卡），
 * 这样从提示条点「立即更新」之后切页面，更新流程不会被卸载掉。
 *
 * ⚠️ 已知限制（**没有处理**，别以为处理过了）：状态只活在内存里。
 * applying / restarting 期间用户按 F5 刷新页面，这里的状态机整个丢失，界面会退回
 * 「有新版本，可更新」——而后端其实正在下载或者已经装完在重启。用户此时再点一次
 * 「立即更新」，就是把同一个版本重下一遍。
 * 要真正修掉它，后端必须暴露「我正在更新中」这个状态（例如 GET /api/update 里带一个
 * inProgress 字段，或者一个独立的进度端点），前端挂载时据此把状态机接回去 ——
 * 那是契约变更，不是前端能单方面解决的。在此之前，唯一的缓解是「不要给出会重复下载
 * 的按钮」，见 isSettledPhase。
 */
export function useSelfUpdate(options: UseSelfUpdateOptions = {}): UseSelfUpdateResult {
  const {
    apply = applyUpdate,
    health = fetchHealth,
    reload = reloadPage,
    pollIntervalMs = HEALTH_POLL_INTERVAL_MS,
    restartTimeoutMs = RESTART_TIMEOUT_MS,
    reloadDelayMs = RELOAD_DELAY_MS,
  } = options

  const [state, setState] = useState<SelfUpdateState>({ phase: 'idle' })
  const [elapsedSec, setElapsedSec] = useState(0)

  // 注入的函数走 ref：调用方每次渲染传新的函数字面量也不会重启轮询 effect
  const depsRef = useRef({ apply, health, reload })
  useEffect(() => {
    depsRef.current = { apply, health, reload }
  }, [apply, health, reload])

  // 稳定回调里要读到当前阶段（重复点「立即更新」时据此挡掉第二次 apply）
  const stateRef = useRef(state)
  useEffect(() => {
    stateRef.current = state
  }, [state])

  // 会话代次。每次发起更新都 +1，在途的 apply / health 响应据此丢弃 ——
  // 失败后立刻重试时，上一轮那个还挂在网络上的 apply 回来时不能再动状态机。
  const genRef = useRef(0)

  const start = useCallback(() => {
    if (isBusyPhase(stateRef.current.phase)) return
    genRef.current += 1
    const gen = genRef.current
    setState({ phase: 'applying' })
    setElapsedSec(0)

    void (async () => {
      try {
        const data = await depsRef.current.apply()
        if (gen !== genRef.current) return
        setState({ phase: 'restarting', version: data.version })
        setElapsedSec(0)
      } catch (err) {
        if (gen !== genRef.current) return
        console.error('一键更新失败', err)
        setState({ phase: 'error', message: errorText(err, '更新失败') })
      }
    })()
  }, [])

  // 走秒表。没有服务端进度可用，一个每秒都在变的数字是「还活着」与
  // 「卡死了」之间唯一能区分的信号 —— 静止的 spinner 两者长得一模一样。
  const { phase } = state
  useEffect(() => {
    if (phase !== 'applying' && phase !== 'restarting') return
    const timer = setInterval(() => setElapsedSec((sec) => sec + 1), ELAPSED_TICK_MS)
    return () => clearInterval(timer)
  }, [phase])

  // 等后端重启：1 秒一次问 health，直到版本号对上或者超时。
  // state 整个进 deps 是安全的：restarting 的状态对象只被 setState 一次，
  // 走秒表的重渲染不会换掉它的引用。
  useEffect(() => {
    if (state.phase !== 'restarting') return
    const gen = genRef.current
    const expected = state.version
    const maxAttempts = Math.max(1, Math.ceil(restartTimeoutMs / pollIntervalMs))
    let attempts = 0
    let inFlight = false

    async function probe(): Promise<void> {
      if (inFlight) return
      inFlight = true
      attempts += 1
      const attempt = attempts
      const back = await isBackUp(depsRef.current.health, expected)
      inFlight = false
      if (gen !== genRef.current) return
      if (back) {
        setState({ phase: 'done', version: expected })
        return
      }
      // 时限到了才收手。这不是失败：新版本确实已经装好了。
      if (attempt >= maxAttempts) {
        setState({ phase: 'timeout', version: expected })
      }
    }

    // 第一拍等满一个间隔再发：apply 刚返回时后端还没开始重启，
    // 立刻问只会问到正在退出的旧进程，白白吃掉一次尝试。
    const timer = setInterval(() => {
      void probe()
    }, pollIntervalMs)
    return () => clearInterval(timer)
  }, [state, pollIntervalMs, restartTimeoutMs])

  // 后端换了版本，前端产物也换了 —— 必须整页刷新，否则旧 JS 会一直打新后端。
  // 停一下再刷，让用户看清「已更新到 vX」而不是页面莫名其妙一闪。
  useEffect(() => {
    if (state.phase !== 'done') return
    const timer = setTimeout(() => depsRef.current.reload(), reloadDelayMs)
    return () => clearTimeout(timer)
  }, [state.phase, reloadDelayMs])

  return {
    state,
    elapsedSec: isBusyPhase(state.phase) ? elapsedSec : 0,
    busy: isBusyPhase(state.phase),
    start,
  }
}

/**
 * 问一次 /api/health，判断「带着新版本号的后端起来了没」。
 *
 * 重启期间请求失败是**预期内**的（连接被拒 / 半死的进程），所以这里吞掉异常
 * 只回 false —— 不落错误态、不打日志，否则重启的头几秒会刷出一串假故障，
 * 而那正是用户最紧张的几秒。真正等不回来由外层的总时限兜底，届时给出
 * 明确的中文提示，失败路径不会静默消失。
 */
async function isBackUp(health: () => Promise<HealthData>, expected: string): Promise<boolean> {
  try {
    const data = await health()
    // 正在退出的旧进程也答得上 health，所以必须比对版本号才算「新的起来了」。
    // 契约异常（apply 没给版本号）时退化成「答得上就算」，否则永远等不到。
    if (normalizeVersion(expected) === '') return true
    return normalizeVersion(data.version) === normalizeVersion(expected)
  } catch {
    return false
  }
}

/** 默认的刷新方式；测试注入替身，不能真的重载 jsdom */
function reloadPage(): void {
  window.location.reload()
}

/** 由根布局提供；null 表示没套 Provider（属于装配错误，直接报） */
export const SelfUpdateContext = createContext<UseSelfUpdateResult | null>(null)

export function useSelfUpdateContext(): UseSelfUpdateResult {
  const value = useContext(SelfUpdateContext)
  if (value === null) {
    throw new Error('useSelfUpdateContext 必须在 SelfUpdateContext.Provider 之内使用（见 RootLayout）')
  }
  return value
}
