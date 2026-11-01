import { useEffect, useState } from 'react'
import type { CatalogFreshness } from '../../lib/endpoints'
import { freshnessNotable, freshnessText } from '../../lib/freshness'

/** 「N 分钟前」每分钟走一格就够了 */
const TICK_MS = 60_000

interface FreshnessNoteProps {
  freshness: CatalogFreshness | undefined
  /** 拿到过期的一份后、正在等那一次后台重取 */
  refreshing?: boolean
  className?: string
}

/**
 * 「离线 · 更新于 2 小时前」细条（DESIGN.md「状态表」：离线时显示上次缓存的内容）。
 * 只在离线或过期时出现；后台刷新换上新数据后，它让出的位置保留到这一页离开为止 ——
 * 文字消失但不把下面的内容往上拽（不跳版）。
 */
export function FreshnessNote({ freshness, refreshing = false, className }: FreshnessNoteProps) {
  const notable = freshnessNotable(freshness)
  const [shown, setShown] = useState(notable)
  const [now, setNow] = useState(() => Date.now())
  // 出现过一次就记住：之后数据变新，原位留一个等高的空位
  if (notable && !shown) setShown(true)

  // 换了一份数据（fetchedAt 变了）时立刻按当前时间重算，之后每分钟走一格
  const fetchedAt = freshness?.fetchedAt
  useEffect(() => {
    if (!notable) return
    setNow(Date.now())
    const timer = window.setInterval(() => setNow(Date.now()), TICK_MS)
    return () => window.clearInterval(timer)
  }, [notable, fetchedAt])

  const classes = ['freshness-note', className].filter(Boolean).join(' ')
  if (!notable) return shown ? <p className={`${classes} freshness-note--cleared`} aria-hidden="true" /> : null
  return (
    <p className={classes} role="status" data-offline={freshness.offline || undefined}>
      <span className="freshness-note-dot" aria-hidden="true" />
      {freshnessText(freshness, now, refreshing)}
    </p>
  )
}
