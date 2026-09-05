/** 与原版评分分段一致，评分统一使用 0–100。 */
export function audienceColor(score: number) {
  return score < 40 ? '#fca5a5' : score < 60 ? '#fcd34d' : score < 70 ? '#bef264' : score < 82 ? '#6ee7b7' : '#a5b4fc'
}
export function airingDistance(at: number, now = Date.now()) {
  const seconds = Math.abs(at * 1000 - now) / 1000
  const amount = seconds >= 86400 ? `${Math.max(1, Math.round(seconds / 86400))} 天` : seconds >= 3600 ? `${Math.max(1, Math.round(seconds / 3600))} 小时` : `${Math.max(1, Math.round(seconds / 60))} 分钟`
  return amount + (at * 1000 >= now ? '后' : '前')
}
