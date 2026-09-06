import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchTorrentStatus, startTorrentPlay, stopTorrent } from '../lib/endpoints'
import type {
  DanmakuStatus,
  TorrentFile,
  TorrentPlayData,
  TorrentPlayRequest,
  TorrentStatus,
} from '../lib/endpoints'
import { errorText } from '../lib/format'

/** 播放流程进行中每隔多久拉一次 /api/torrent/status */
export const TORRENT_POLL_INTERVAL_MS = 1000

/**
 * 一次磁力播放流程的状态机。
 * starting → （needSelection）selecting → starting → streaming，任何一步失败落到 error。
 * magnet / title 一路带着，才能在选集与重试时不依赖界面把参数再传一遍。
 */
export type TorrentPlayState =
  | { phase: 'idle' }
  | { phase: 'starting'; magnet: string; title: string }
  | { phase: 'selecting'; magnet: string; title: string; files: TorrentFile[] }
  | { phase: 'streaming'; magnet: string; title: string; danmaku: DanmakuStatus }
  | { phase: 'error'; magnet: string; title: string; message: string }

export interface UseTorrentPlayOptions {
  /** 可注入的请求函数（测试替身用）；默认走真实 API */
  start?: (request: TorrentPlayRequest, signal?: AbortSignal) => Promise<TorrentPlayData>
  poll?: () => Promise<TorrentStatus>
  stop?: () => Promise<void>
  /** 轮询间隔毫秒（测试可调小）；默认 1 秒 */
  intervalMs?: number
}

export interface UseTorrentPlayResult {
  state: TorrentPlayState
  /** 最近一次轮询到的后端状态；null = 还没拉到或上次拉取失败 */
  status: TorrentStatus | null
  /** 已连续多少秒没有连接到任何分享者；0 表示当前有 peer（或还没开始轮询） */
  zeroPeerSeconds: number
  /** 是否有一次磁力播放流程占着后端 —— 其余结果行的播放按钮据此禁用 */
  busy: boolean
  /** 发起播放；媒体入口可把目标集数随完整请求带进来。 */
  play: (request: TorrentPlayRequest | string, title: string) => void
  /** 用户在选集弹窗里选定文件后，带 fileIndex 重发 */
  selectFile: (fileIndex: number) => void
  /** 失败后重试同一条磁力 */
  retry: () => void
  /**
   * 取消缓冲 / 停止播放 / 放弃选集：让后端释放种子。
   * 界面先回到空闲（前端确实不再等待了），停止失败时 reject，由调用方就地提示 ——
   * 后端可能还留着种子，用户需要知道并有下一步动作（设置页清空缓存）。
   */
  cancel: () => Promise<void>
}

/** 播放流程占着后端的三个阶段（此期间不允许发起第二条磁力） */
function isBusyPhase(phase: TorrentPlayState['phase']): boolean {
  return phase === 'starting' || phase === 'selecting' || phase === 'streaming'
}

/**
 * 磁力播放编排 hook。
 *
 * POST /api/torrent/play 是阻塞请求（等元数据 + 起播缓冲，10 秒到 2 分钟都正常），
 * 期间靠 1 秒轮询 /api/torrent/status 把进展显示出来。用户取消时既 abort 掉那个
 * 长请求（否则挂起的连接会吃满浏览器的并发额度），也 POST stop 让后端释放种子。
 */
export function useTorrentPlay(options: UseTorrentPlayOptions = {}): UseTorrentPlayResult {
  const {
    start = startTorrentPlay,
    poll = fetchTorrentStatus,
    stop = stopTorrent,
    intervalMs = TORRENT_POLL_INTERVAL_MS,
  } = options

  const [state, setState] = useState<TorrentPlayState>({ phase: 'idle' })
  const [status, setStatus] = useState<TorrentStatus | null>(null)
  const [zeroPeerTicks, setZeroPeerTicks] = useState(0)

  // 注入的函数走 ref：调用方每次渲染传新的函数字面量也不会重启轮询 effect
  const depsRef = useRef({ start, poll, stop })
  useEffect(() => {
    depsRef.current = { start, poll, stop }
  }, [start, poll, stop])

  // 稳定回调里要读到当前状态（选集 / 重试都要用状态里带的 magnet）
  const stateRef = useRef(state)
  useEffect(() => {
    stateRef.current = state
  }, [state])

  const abortRef = useRef<AbortController | null>(null)
  // 选集与重试必须复用最初的完整请求。只把 magnet/title 放在可见状态里会在
  // 第二次 POST 时丢掉媒体入口传来的 episodeHint，最终可能选中合集里的错误文件。
  const requestRef = useRef<TorrentPlayRequest | null>(null)
  const inFlightRef = useRef(false)
  // 会话代次。每次发起播放或取消都 +1，轮询据此丢弃属于旧会话的响应。
  // 没有它会出这样的 bug：取消 A 后立刻播 B，A 那次在途的轮询回来时带着
  // active=false，而此时 phase 又是 'streaming'（B 的），收摊判定就命中了
  // ——mpv 还在播 B，界面却把状态条收了、按钮全放开。
  const genRef = useRef(0)

  // newGeneration 作废当前会话：轮询结果按代次丢弃，并放开轮询互斥，
  // 让新会话的第一次轮询不必等旧的那次网络往返结束。
  const newGeneration = useCallback(() => {
    genRef.current += 1
    inFlightRef.current = false
  }, [])

  // 卸载时掐断在途的长请求，别让它回来对着已卸载的组件 setState
  useEffect(() => {
    return () => {
      abortRef.current?.abort()
      abortRef.current = null
    }
  }, [])

  const tick = useCallback(async () => {
    if (inFlightRef.current) return
    inFlightRef.current = true
    const gen = genRef.current
    try {
      const next = await depsRef.current.poll()
      // 代次变了 = 这次轮询属于已经被取消或替换掉的会话，一个字都不能用。
      if (gen !== genRef.current) return
      setStatus(next)
      setZeroPeerTicks((ticks) => (next.peers > 0 ? 0 : ticks + 1))
      // 播放中后端说会话没了（从库页停了播放、或播放结束清理掉了）→ 界面收摊。
      // 只在 streaming 判定：starting 期间后端可能还没建好会话就先返回 active=false，
      // 那时权威的是 POST /play 的返回值，不是轮询。
      if (!next.active && stateRef.current.phase === 'streaming') {
        setState({ phase: 'idle' })
        setStatus(null)
        setZeroPeerTicks(0)
        requestRef.current = null
      }
    } catch (err) {
      if (gen !== genRef.current) return
      // 轮询失败只让进度显示变「未知」，不打断播放流程本身（POST /play 才是权威）
      console.error('拉取磁力状态失败', err)
      setStatus(null)
    } finally {
      // 只有仍属当前代次的那次轮询才交还互斥；旧会话的轮询在 newGeneration
      // 里已经放开过了，这里再放一次会误开新会话正持有的那把。
      if (gen === genRef.current) inFlightRef.current = false
    }
  }, [])

  const isPolling = isBusyPhase(state.phase)
  useEffect(() => {
    if (!isPolling) return
    void tick() // 立刻拉一次，别让状态条空着等第一个间隔
    const timer = setInterval(() => {
      void tick()
    }, intervalMs)
    return () => clearInterval(timer)
  }, [isPolling, intervalMs, tick])

  const run = useCallback((request: TorrentPlayRequest, title: string) => {
    abortRef.current?.abort()
    newGeneration()
    const controller = new AbortController()
    abortRef.current = controller
    requestRef.current = request

    setState({ phase: 'starting', magnet: request.magnet, title })
    setStatus(null)
    setZeroPeerTicks(0)

    void (async () => {
      try {
        const data = await depsRef.current.start(request, controller.signal)
        if (controller.signal.aborted) return
        if (data.needSelection) {
          setState({ phase: 'selecting', magnet: request.magnet, title, files: data.files })
          return
        }
        setState({
          phase: 'streaming',
          magnet: request.magnet,
          title: data.title,
          danmaku: data.danmaku,
        })
      } catch (err) {
        // 用户取消 / 组件卸载导致的 abort 不是错误，别弹「无法连接到后端」
        if (controller.signal.aborted) return
        console.error('磁力播放失败', err)
        setState({
          phase: 'error',
          magnet: request.magnet,
          title,
          message: errorText(err, '播放失败'),
        })
      } finally {
        if (abortRef.current === controller) abortRef.current = null
      }
    })()
  }, [newGeneration])

  const play = useCallback(
    (request: TorrentPlayRequest | string, title: string) => {
      run(typeof request === 'string' ? { magnet: request, title } : request, title)
    },
    [run],
  )

  const selectFile = useCallback(
    (fileIndex: number) => {
      const current = stateRef.current
      if (current.phase !== 'selecting') return
      const request = requestRef.current
      if (request === null) return
      run({ ...request, fileIndex }, current.title)
    },
    [run],
  )

  const retry = useCallback(() => {
    const current = stateRef.current
    if (current.phase !== 'error') return
    const request = requestRef.current
    if (request !== null) run(request, current.title)
  }, [run])

  const cancel = useCallback(async () => {
    const wasBusy = stateRef.current.phase !== 'idle'
    abortRef.current?.abort()
    abortRef.current = null
    newGeneration()
    setState({ phase: 'idle' })
    setStatus(null)
    setZeroPeerTicks(0)
    requestRef.current = null
    if (!wasBusy) return
    // 即使 play 是失败结束的也要 stop：后端可能已经把种子加进来了（缓冲超时那条路径）
    await depsRef.current.stop()
  }, [newGeneration])

  return {
    state,
    status,
    zeroPeerSeconds: Math.round((zeroPeerTicks * intervalMs) / 1000),
    busy: isBusyPhase(state.phase),
    play,
    selectFile,
    retry,
    cancel,
  }
}
