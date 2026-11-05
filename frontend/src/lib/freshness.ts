import type { CatalogFreshness } from './endpoints'

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
/** 超过这么久就不再说「N 天前」，直接给日期 */
const RELATIVE_LIMIT = 30 * DAY

/**
 * 「更新于 …」里的相对时间：刚刚 / N 分钟前 / N 小时前 / N 天前，再早给日期。
 * 时间解析不了返回 null（调用方换一句不带时间的话）；来自「未来」的时间（时钟回拨）按「刚刚」。
 */
export function relativeTime(iso: string, now = Date.now()): string | null {
  const at = Date.parse(iso)
  if (!Number.isFinite(at)) return null
  const ago = now - at
  if (ago < MINUTE) return '刚刚'
  if (ago < HOUR) return `${Math.floor(ago / MINUTE)} 分钟前`
  if (ago < DAY) return `${Math.floor(ago / HOUR)} 小时前`
  if (ago < RELATIVE_LIMIT) return `${Math.floor(ago / DAY)} 天前`
  const date = new Date(at)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

/**
 * 目录数据的来历要不要告诉用户：离线（上游连不上，显示的是上一份）或过期（后台正在刷新）时要；
 * 只是「来自上次运行留在盘上的一份、但还在缓存时限内」不提 —— 那份内容是新的。
 */
export function freshnessNotable(freshness: CatalogFreshness | undefined): freshness is CatalogFreshness {
  return freshness !== undefined && (freshness.offline || freshness.stale)
}

/**
 * 状态条上的一句话（DESIGN.md「状态表」）：离线 · 更新于 2 小时前。
 * 只是过期（没离线）时不说「离线」：refreshing = 后台那一次重取还没回来，说正在刷新；回来了仍是旧的就只说更新于何时。
 */
export function freshnessText(freshness: CatalogFreshness, now = Date.now(), refreshing = false): string {
  const when = relativeTime(freshness.fetchedAt, now)
  const updated = when === null ? '显示的是上次保存的内容' : `更新于 ${when}`
  if (freshness.offline) return `离线 · ${updated}`
  return refreshing ? `${updated} · 正在刷新` : updated
}

/**
 * 过期且在线时要不要在后台再取一次：后端此刻正在刷新这一份（stale-while-revalidate），
 * 稍后再问就能拿到新的。离线（刷新刚失败过）时不问 —— 后端一分钟内不会再试，问了也是同一份。
 */
export function shouldRefetchStale(freshness: CatalogFreshness | undefined, online = true): boolean {
  return freshness !== undefined && freshness.stale && !freshness.offline && online
}
