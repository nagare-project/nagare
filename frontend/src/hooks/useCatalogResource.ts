import { useCallback, useEffect, useEffectEvent, useState } from 'react'
import type { CatalogFreshness } from '../lib/endpoints'
import { errorText } from '../lib/format'
import { shouldRefetchStale } from '../lib/freshness'

/**
 * 拿到「过期、后台正在刷新」的一份之后，等多久再问一次。
 * 后端的刷新通常一两秒就落定（上游读取的期限是 20 秒）；问早了还是旧的，问一次就停，不轮询。
 */
export const STALE_REFETCH_MS = 5000

export interface CatalogResource<T> {
  data: T | undefined
  error: string | undefined
  /** 还没有数据、也没有错误（首次加载，或换了 key 之后） */
  loading: boolean
  /** 拿到过期的一份后，正在等那一次后台重取 */
  refreshing: boolean
  /** 出错后重新加载（会回到 loading） */
  retry: () => void
}

export interface CatalogResourceOptions {
  /** 失败时的兜底文案（后端没给中文原因时） */
  fallbackError: string
  /** 后台定期重取的间隔；缺省不定期重取 */
  refreshMs?: number
  /** 窗口重新获得焦点时在后台重取 */
  refreshOnFocus?: boolean
}

interface ResourceState<T> {
  key: string
  data?: T
  error?: string
  refreshing?: boolean
}

/**
 * 目录类页面（发现 / 季度 / 放送表）的读取：
 * - key 变了（换了季度）就清掉旧数据重新读，界面回到骨架；
 * - 后台重取（过期后的那一次、定时、回到窗口）成功时原地换上新数据，不经过骨架、不跳版；
 *   失败时留着已经显示的内容，只记一条日志 —— 一次刷新失败不该把整页换成错误页；
 * - 数据带 `freshness.stale` 且没有离线时，等 STALE_REFETCH_MS 再问一次（DESIGN.md「状态表」）。
 */
export function useCatalogResource<T extends { freshness?: CatalogFreshness }>(
  key: string,
  load: (signal: AbortSignal) => Promise<T>,
  { fallbackError, refreshMs, refreshOnFocus = false }: CatalogResourceOptions,
): CatalogResource<T> {
  const [state, setState] = useState<ResourceState<T>>({ key })
  const [attempt, setAttempt] = useState(0)
  const fetchLatest = useEffectEvent((signal: AbortSignal) => load(signal))
  const fallback = useEffectEvent(() => fallbackError)

  useEffect(() => {
    const abort = new AbortController()
    let seq = 0
    let staleTimer: number | undefined
    let staleChecked = false
    let hasData = false
    setState({ key })

    const run = (background: boolean) => {
      const current = ++seq
      fetchLatest(abort.signal)
        .then((data) => {
          if (abort.signal.aborted || current !== seq) return
          // 过期的一份只追问一次：追问回来的仍是过期的，就照实显示「更新于 …」，不再轮询
          const refetch = !staleChecked && shouldRefetchStale(data.freshness, navigator.onLine !== false)
          staleChecked = true
          hasData = true
          setState({ key, data, refreshing: refetch })
          if (refetch) staleTimer = window.setTimeout(() => run(true), STALE_REFETCH_MS)
        })
        .catch((err: unknown) => {
          if (abort.signal.aborted || current !== seq) return
          if (background) {
            console.warn('目录后台刷新失败，继续显示已有内容', err)
            setState((prev) => (prev.key === key ? { ...prev, refreshing: false } : prev))
            return
          }
          setState({ key, error: errorText(err, fallback()) })
        })
    }
    run(false)

    // 还没读到任何东西（首次加载中或已出错）时不在后台补；那由页面的「重试」负责
    const refresh = () => {
      if (hasData) run(true)
    }
    const timer = refreshMs ? window.setInterval(refresh, refreshMs) : undefined
    if (refreshOnFocus) window.addEventListener('focus', refresh)
    return () => {
      abort.abort()
      window.clearTimeout(staleTimer)
      window.clearInterval(timer)
      if (refreshOnFocus) window.removeEventListener('focus', refresh)
    }
  }, [key, attempt, refreshMs, refreshOnFocus])

  const retry = useCallback(() => setAttempt((n) => n + 1), [])
  // key 刚变、effect 还没跑的那一次渲染里，不能把上一个 key 的数据当成这一个的
  const current = state.key === key ? state : { key }
  return {
    data: current.data,
    error: current.error,
    loading: current.data === undefined && current.error === undefined,
    refreshing: current.refreshing === true,
    retry,
  }
}
