import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchPlayerStatus } from '../lib/endpoints'
import type { PlayerStatus } from '../lib/endpoints'
import { errorText } from '../lib/format'

/** 播放中每隔多久拉一次 /api/player/status */
export const PLAYER_POLL_INTERVAL_MS = 2000

export interface UsePlayerStatusOptions {
  /** 可注入的取状态函数（测试替身用）；默认走真实 API */
  fetcher?: () => Promise<PlayerStatus>
  /** 轮询间隔毫秒（测试可调小）；默认 2 秒 */
  intervalMs?: number
}

export interface UsePlayerStatusResult {
  /** 最近一次拿到的播放器状态；null = 尚未取到或上次拉取失败（状态未知） */
  status: PlayerStatus | null
  /** 拉取失败时的用户可读文案；成功后自动清空 */
  error: string | null
  /** 立即拉取一次（play/pause/stop 等变更后调用，让界面即时跟上） */
  refresh: () => Promise<void>
  /** 用已知状态直接覆盖本地（POST /api/play 成功后的乐观更新），并唤醒轮询 */
  apply: (next: PlayerStatus) => void
}

/**
 * 播放器状态轮询 hook。
 *
 * 规则：挂载先拉一次；只要当前状态是 playing 就每 intervalMs 轮询一次，
 * 变为非播放后自动停表。拉取失败时把状态置回「未知」（同时停表，避免对着
 * 已退出的后端无限刷错误日志），错误文案透出给界面展示。
 */
export function usePlayerStatus(options: UsePlayerStatusOptions = {}): UsePlayerStatusResult {
  const { fetcher = fetchPlayerStatus, intervalMs = PLAYER_POLL_INTERVAL_MS } = options
  const [status, setStatus] = useState<PlayerStatus | null>(null)
  const [error, setError] = useState<string | null>(null)

  // fetcher 走 ref：调用方每次渲染传新函数字面量也不会重启轮询 effect
  const fetcherRef = useRef(fetcher)
  useEffect(() => {
    fetcherRef.current = fetcher
  }, [fetcher])

  // 同一时刻只允许一个在途请求：本地 API 慢到超过轮询间隔时直接跳过该拍
  const inFlightRef = useRef(false)

  const refresh = useCallback(async () => {
    if (inFlightRef.current) return
    inFlightRef.current = true
    try {
      const next = await fetcherRef.current()
      setStatus(next)
      setError(null)
    } catch (err) {
      console.error('拉取播放器状态失败', err)
      setStatus(null)
      setError(errorText(err, '无法获取播放器状态'))
    } finally {
      inFlightRef.current = false
    }
  }, [])

  // 挂载先拉一次：覆盖「上个会话的 mpv 还在放」的场景
  useEffect(() => {
    void refresh()
  }, [refresh])

  const isPlaying = status?.playing === true
  useEffect(() => {
    if (!isPlaying) return
    const timer = setInterval(() => {
      void refresh()
    }, intervalMs)
    return () => clearInterval(timer)
  }, [isPlaying, intervalMs, refresh])

  const apply = useCallback((next: PlayerStatus) => {
    setStatus(next)
    setError(null)
  }, [])

  return { status, error, refresh, apply }
}
