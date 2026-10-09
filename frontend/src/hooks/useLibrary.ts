import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiAuthError } from '../lib/api'
import {
  addLibraryFolder,
  fetchLibrary,
  removeLibraryFolder,
  rescanLibrary,
} from '../lib/endpoints'
import type { AddFolderData, LibraryData, ScanStats } from '../lib/endpoints'
import { errorText } from '../lib/format'

/** 媒体库加载状态机 */
export type LibraryState =
  | { phase: 'loading' }
  | { phase: 'unauthorized' }
  | { phase: 'error'; message: string }
  | { phase: 'ready'; data: LibraryData }

export interface UseLibraryResult {
  state: LibraryState
  /** 重新拉取列表；已 ready 时保留旧数据直到新数据到达（不闪 loading） */
  reload: () => Promise<void>
  /** 添加文件夹并刷新列表；失败原样抛出（信封中文错误），由表单就地展示 */
  addFolder: (path: string) => Promise<AddFolderData>
  /** 删除文件夹并刷新列表；失败原样抛出 */
  removeFolder: (id: string) => Promise<void>
  /** 触发全量重扫并刷新列表，返回扫描统计；失败原样抛出 */
  rescan: () => Promise<ScanStats>
}

/** 后台识别作品期间多久刷新一次（封面一张张出来；识别本身按 animego 限速一秒一个） */
export const IDENTIFY_POLL_MS = 3000
/** 识别因为 animego 不可用暂停时多久刷新一次：后端到点会自己重试，页面要能看到它恢复 */
export const IDENTIFY_PAUSED_POLL_MS = 30_000

export interface UseLibraryOptions {
  /**
   * 后台识别作品期间定时刷新（只给显示识别进度的媒体库页用）。其余页面不轮询：
   * 它们不显示进度，每三秒拉一整份媒体库是白费；而且刷新失败不能把页面内容换成错误页。
   */
  watchIdentify?: boolean
}

/**
 * 媒体库数据 hook：GET /api/library 的状态机 + 三个变更动作。
 * 变更动作成功后自动 reload，让列表始终反映后端最新扫描结果；
 * 动作的失败不进状态机（页面主体还好好的），抛给调用方就地提示。
 */
export function useLibrary({ watchIdentify = false }: UseLibraryOptions = {}): UseLibraryResult {
  const [state, setState] = useState<LibraryState>({ phase: 'loading' })
  // 请求序号：只采用最新一次请求的结果。慢的旧响应晚到时会把新数据盖回去
  //（比如盖掉「正在识别」，轮询就此停下；或盖掉刚改完的作品关联）
  const latest = useRef(0)

  /**
   * background：后台定时刷新。失败时保留已经显示的内容（只记日志）——
   * 一次刷新失败（自更新重启、偶发 5xx）不该把页面换成错误页、打断正在浏览器里播的视频。
   */
  const load = useCallback(async (background: boolean) => {
    const seq = ++latest.current
    try {
      const data = await fetchLibrary()
      if (seq === latest.current) setState({ phase: 'ready', data })
    } catch (err) {
      if (seq !== latest.current) return
      if (err instanceof ApiAuthError) {
        setState({ phase: 'unauthorized' })
        return
      }
      console.error('读取媒体库失败', err)
      const message = errorText(err, '读取媒体库失败')
      setState((prev) => (background && prev.phase === 'ready' ? prev : { phase: 'error', message }))
    }
  }, [])
  const reload = useCallback(() => load(false), [load])

  useEffect(() => {
    void reload()
  }, [reload])

  // 扫描之后后台在识别作品：期间定时刷新，识别完的那一次响应里不再带 identify，轮询自然停下；
  // 识别因为 animego 不可用暂停时放慢刷新，等它到点恢复。标签页不可见时不刷新，切回来立刻补一次
  const identify = state.phase === 'ready' ? state.data.identify : undefined
  const pollMs = !watchIdentify ? 0 : identify?.running ? IDENTIFY_POLL_MS : identify?.error ? IDENTIFY_PAUSED_POLL_MS : 0
  useEffect(() => {
    if (pollMs === 0) return
    let timer: number | undefined
    const schedule = () => {
      window.clearTimeout(timer)
      if (document.visibilityState !== 'hidden') timer = window.setTimeout(() => void load(true), pollMs)
    }
    const onVisible = () => {
      if (document.visibilityState === 'visible') void load(true)
      else window.clearTimeout(timer)
    }
    schedule()
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [pollMs, state, load])

  const addFolder = useCallback(
    async (path: string) => {
      const result = await addLibraryFolder(path)
      await reload()
      return result
    },
    [reload],
  )

  const removeFolder = useCallback(
    async (id: string) => {
      await removeLibraryFolder(id)
      await reload()
    },
    [reload],
  )

  const rescan = useCallback(async () => {
    const { stats } = await rescanLibrary()
    await reload()
    return stats
  }, [reload])

  return { state, reload, addFolder, removeFolder, rescan }
}
