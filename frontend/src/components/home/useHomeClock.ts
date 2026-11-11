import { useSyncExternalStore } from 'react'

// 发现页的时钟：本机的时间，按分钟走（「现在 20:40」在墙上的钟翻页时跟着翻）。
// 时区就是本机的 —— nagare 跑在用户自己的机器上。

const TICK_MS = 60_000

/** 每到整分钟通知一次 */
function subscribeMinute(onChange: () => void): () => void {
  let id: ReturnType<typeof setTimeout>
  const schedule = () => {
    id = setTimeout(() => {
      onChange()
      schedule()
    }, TICK_MS - (Date.now() % TICK_MS) + 50)
  }
  schedule()
  return () => clearTimeout(id)
}

const currentMinute = () => Math.floor(Date.now() / TICK_MS)

/** 当前这一分钟的起点（毫秒） */
export function useHomeClock(): number {
  return useSyncExternalStore(subscribeMinute, currentMinute, currentMinute) * TICK_MS
}
