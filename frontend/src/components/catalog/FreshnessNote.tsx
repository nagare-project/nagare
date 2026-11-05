import { useEffect, useRef, useState } from 'react'
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

/** 从离线回到在线时念的一句（细条随后消失或换成「更新于 …」，不再念） */
export const BACK_ONLINE_TEXT = '已重新连上目录服务'

/**
 * 「离线 · 更新于 2 小时前」细条（DESIGN.md「状态表」：离线时显示上次缓存的内容）。
 * 只在离线或过期时出现；后台刷新换上新数据后，它让出的位置保留到这一页离开为止 ——
 * 文字消失但不把下面的内容往上拽（不跳版）。
 *
 * 看得见的那句不是 live region：相对时间每分钟走一格、「正在刷新」来来去去，挂 role="status"
 * 的话读屏每分钟被打断一次。读屏只在进入离线、离开离线时各听到一句，播报在旁边一个常驻的
 * visually-hidden 区里（先挂空的、再填字，读屏才会念）。
 */
export function FreshnessNote({ freshness, refreshing = false, className }: FreshnessNoteProps) {
  const notable = freshnessNotable(freshness)
  const offline = freshness?.offline === true
  const [shown, setShown] = useState(notable)
  const [now, setNow] = useState(() => Date.now())
  const [announcement, setAnnouncement] = useState('')
  // 上一次播报时是不是离线；挂载时当作在线，挂上来就离线的一份算「进入离线」
  const announcedOffline = useRef(false)
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

  // 只在进出离线的那一下换播报的字；之后的变化（时间走格、换数据）都不碰它
  useEffect(() => {
    if (offline === announcedOffline.current) return
    announcedOffline.current = offline
    setAnnouncement(offline && freshness !== undefined ? freshnessText(freshness, Date.now()) : BACK_ONLINE_TEXT)
  }, [offline, freshness])

  const classes = ['freshness-note', className].filter(Boolean).join(' ')
  const live = <span className="visually-hidden" role="status">{announcement}</span>
  if (!notable) return <>{shown && <p className={`${classes} freshness-note--cleared`} aria-hidden="true" />}{live}</>
  return (
    <>
      <p className={classes} data-offline={freshness.offline || undefined}>
        <span className="freshness-note-dot" aria-hidden="true" />
        {freshnessText(freshness, now, refreshing)}
      </p>
      {live}
    </>
  )
}
