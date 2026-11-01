import { useEffect, useState } from 'react'
import { airingDistance } from './media-format'
import type { MediaSummary } from './types'

/** 上游季度名（WINTER…）→ 卡片上的「冬 / 春 / 夏 / 秋」 */
export const SEASON_ZH: Record<string, string> = { WINTER: '冬', SPRING: '春', SUMMER: '夏', FALL: '秋' }

/**
 * 卡片标题下那一行（DESIGN.md「海报卡」：「12 集 · 已看 7」）：年份季度 · 集数 · 看到哪了。
 * 剧场版不写集数；什么都不知道时返回空串（不画这一行）。
 */
export function cardMeta(media: MediaSummary): string {
  const parts: string[] = []
  const season = media.season ? SEASON_ZH[media.season] : undefined
  if (media.year) parts.push(season ? `${media.year} ${season}` : String(media.year))
  if (media.format === 'MOVIE') parts.push('剧场版')
  else if (media.episodes) parts.push(`${media.episodes} 集`)
  if (media.watched > 0) parts.push(`已看 ${media.watched}`)
  return parts.join(' · ')
}

export interface CardStatus {
  /** 封面角标上的短字 */
  short: string
  /** 给读屏的完整说法 */
  long: string
  /** 角标颜色：有新集是只读信息（teal），连载中是 warning，未播出是次要文字 */
  tone: 'info' | 'warning' | 'quiet'
}

/** 封面右上角的状态角标：最近更新了哪一集 > 放送中 > 未播出；已完结不标 */
export function cardStatus(media: MediaSummary, now = Date.now()): CardStatus | undefined {
  if (media.recentAiring) {
    const when = airingDistance(media.recentAiring.at, now)
    return { short: `第 ${media.recentAiring.episode} 集 · ${when}`, long: `第 ${media.recentAiring.episode} 集 ${when}更新`, tone: 'info' }
  }
  if (media.status === 'RELEASING') return { short: '放送中', long: '放送中', tone: 'warning' }
  if (media.status === 'NOT_YET_RELEASED') return { short: '未播出', long: '尚未播出', tone: 'quiet' }
  return undefined
}

/** 浮层卡的版式条件：宽屏、能悬停的指针。再窄的话卡片放不下浮层里的按钮，触屏上没有悬停 */
const POPUP_QUERY = '(min-width: 1280px) and (hover: hover)'

export function usePopupLayout(): boolean {
  const [matches, setMatches] = useState(() => window.matchMedia?.(POPUP_QUERY).matches ?? true)
  useEffect(() => {
    const query = window.matchMedia?.(POPUP_QUERY)
    if (!query) return
    const sync = () => setMatches(query.matches)
    sync()
    query.addEventListener('change', sync)
    return () => query.removeEventListener('change', sync)
  }, [])
  return matches
}
