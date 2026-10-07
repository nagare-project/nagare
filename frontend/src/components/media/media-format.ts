/** 与原版评分分段一致，评分统一使用 0–100。 */
export function audienceColor(score: number) {
  return score < 40 ? '#fca5a5' : score < 60 ? '#fcd34d' : score < 70 ? '#bef264' : score < 82 ? '#6ee7b7' : '#a5b4fc'
}
export function airingDistance(at: number, now = Date.now()) {
  const seconds = Math.abs(at * 1000 - now) / 1000
  const amount = seconds >= 86400 ? `${Math.max(1, Math.round(seconds / 86400))} 天` : seconds >= 3600 ? `${Math.max(1, Math.round(seconds / 3600))} 小时` : `${Math.max(1, Math.round(seconds / 60))} 分钟`
  return amount + (at * 1000 >= now ? '后' : '前')
}

/** 作品类型（AniList format）的中文写法 */
export const FORMAT_LABELS: Record<string, string> = { TV: 'TV', TV_SHORT: '短篇动画', MOVIE: '剧场版', SPECIAL: '特别篇', OVA: 'OVA', ONA: '网络动画' }
