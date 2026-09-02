import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiAuthError } from '../lib/api'
import { searchMagnets } from '../lib/endpoints'
import type { SearchResult } from '../lib/endpoints'
import { errorText } from '../lib/format'

/**
 * 搜索状态机。searching 时带上一次的结果（stale），让「切换源后自动重搜」
 * 不把表格清成一片空白再重新长出来。
 */
export type SearchState =
  | { phase: 'idle' }
  | { phase: 'searching'; query: string; stale: SearchResult | null }
  | { phase: 'unauthorized' }
  | { phase: 'error'; query: string; message: string }
  | { phase: 'ready'; data: SearchResult }

export interface UseMagnetSearchOptions {
  /** 可注入的搜索函数（测试替身用）；默认走真实 API */
  fetcher?: (query: string) => Promise<SearchResult>
}

export interface UseMagnetSearchResult {
  state: SearchState
  /** 用当前关键词再搜一次（切换源启用状态后调用）；关键词为空时不做事 */
  refetch: () => Promise<void>
}

/**
 * 关键词驱动的搜索 hook：query 变化即发起请求，空串回到 idle。
 * 用递增序号丢弃过期响应 —— 用户连续改关键词时，先发后到的旧结果不能盖掉新结果。
 */
export function useMagnetSearch(query: string, options: UseMagnetSearchOptions = {}): UseMagnetSearchResult {
  const { fetcher = searchMagnets } = options
  const [state, setState] = useState<SearchState>({ phase: 'idle' })

  // fetcher 走 ref：调用方每次渲染传新函数字面量也不会重发请求
  const fetcherRef = useRef(fetcher)
  useEffect(() => {
    fetcherRef.current = fetcher
  }, [fetcher])

  const seqRef = useRef(0)
  const lastDataRef = useRef<SearchResult | null>(null)

  const run = useCallback(async (q: string) => {
    seqRef.current += 1
    const seq = seqRef.current
    setState({ phase: 'searching', query: q, stale: lastDataRef.current })
    try {
      const data = await fetcherRef.current(q)
      if (seq !== seqRef.current) return
      lastDataRef.current = data
      setState({ phase: 'ready', data })
    } catch (err) {
      if (seq !== seqRef.current) return
      if (err instanceof ApiAuthError) {
        setState({ phase: 'unauthorized' })
        return
      }
      console.error('搜索失败', err)
      setState({ phase: 'error', query: q, message: errorText(err, '搜索失败') })
    }
  }, [])

  useEffect(() => {
    if (query === '') {
      // 作废在途请求，清掉上次结果：回到未搜索态不该还带着旧表格
      seqRef.current += 1
      lastDataRef.current = null
      setState({ phase: 'idle' })
      return
    }
    void run(query)
  }, [query, run])

  const refetch = useCallback(async () => {
    if (query === '') return
    await run(query)
  }, [query, run])

  return { state, refetch }
}
