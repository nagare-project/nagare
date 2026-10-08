import type { SearchItem, TorrentPlayRequest } from '../../lib/endpoints'
import { parsePublished } from '../../lib/format'
import type { MediaSummary } from './types'

/**
 * 磁力发布 ↔ 作品页上选的那一集。
 *
 * 发布标题里的集号是字幕组自己的编号：按季编号的第二季第 3 集叫 03，跨季连续编号的叫 15
 * （前作 12 集时）。作品集号是目录作品自己的 1..总集数，用户在作品页点的是它。
 * 集号、合集区间都由后端按同一条解析链给出（SearchItem.episode / kind / episodeRange），
 * 这里只做「这条发布是不是这一集」的判断；唯一的例外是后端没认出合集、又解不出集号的发布，
 * 按标题里的 BD 一类字样兜底当合集看（isBatchRelease），只影响展示与选集提示。
 */

/** 只列已经播出的集。未知总集数时依然可以依靠最近/下次放送信息给出已有集数。 */
export function airedEpisodeCount(media: MediaSummary): number {
  if (media.format === 'MOVIE') return 1
  const knownAired = Math.max(
    media.watched,
    media.recentAiring?.episode ?? 0,
    (media.nextAiring?.episode ?? 1) - 1,
  )
  if (media.status === 'NOT_YET_RELEASED') return knownAired
  if (media.status === 'RELEASING' || media.episodes === null) return knownAired
  return Math.max(knownAired, media.episodes ?? 0)
}

/** 默认选中下一集没看的；全看完了停在最后一集；不知道播到哪一集就从下一集算起。 */
export function defaultTorrentEpisode(media: MediaSummary): number {
  const next = Math.max(1, media.watched + 1)
  const aired = airedEpisodeCount(media)
  return aired > 0 ? Math.min(next, aired) : next
}

export const MAX_EPISODE_CHIPS = 120

/** 集号上限，与后端一致（长篇连载过千集，再大就不是集号了） */
export const MAX_EPISODE = 9999

/** 集号按钮：已播出不多时全列；长篇只列靠近当前进度的一段，其余靠输入集号。 */
export function episodeChoices(media: MediaSummary, max = MAX_EPISODE_CHIPS): number[] {
  const count = airedEpisodeCount(media)
  if (count <= max) return Array.from({ length: count }, (_, index) => index + 1)
  const start = Math.max(1, Math.min(count - max + 1, media.watched - 10))
  return Array.from({ length: max }, (_, index) => start + index)
}

/** 作品页上选定的那一集，连同判断发布要用的上下文 */
export interface EpisodeTarget {
  /** 作品集号 */
  episode: number
  /** 跨季连续编号的偏移：前作一共多少集。只有已知且大于 0 时才给 */
  offset?: number
  /**
   * 目录作品是第几季。标题写明了才有；没写时只有确知没有前作（偏移已知为 0）才算第 1 季 ——
   * 「鬼灭之刃 游郭篇」这类用副标题的续作标题里没有季数，按第 1 季算会把标着 S2 的发布全藏掉。
   */
  season?: number
  /** 本季集数（已知总集数与已播出集数取大）；不知道时不给 */
  total?: number
  /**
   * 早于这个时刻（毫秒）发布的不可能是这一季的：作品首播日期往前留 30 天余量（先行放送、
   * 时区）。续作页上搜回来的前作单集「- 01」标题里没有季数，只有发布日期分得开。不知道首播日期时不给
   */
  notBefore?: number
  /** 除正片外也算「这一集」的类型：OVA / 特别篇作品自己的发布本来就标着 OVA、SP */
  extraKinds?: readonly string[]
}

const KINDS_BY_FORMAT: Record<string, readonly string[]> = { OVA: ['ova', 'sp'], SPECIAL: ['sp', 'ova'] }

/** 作品形式决定哪些类型也算这一集（只有 OVA、特别篇需要） */
export function extraKindsFor(format: string | undefined): readonly string[] | undefined {
  return format === undefined ? undefined : KINDS_BY_FORMAT[format]
}

function countsAsEpisode(kind: string | undefined, target: EpisodeTarget | null): boolean {
  return kind === undefined || kind === 'main' || (target?.extraKinds?.includes(kind) ?? false)
}

/**
 * 发布与选定集的关系：
 * - exact：单集，集号就是这一集；offset：单集，按跨季连续编号就是这一集；
 * - batch：合集，写明的区间含这一集；batch-unknown：合集，没写区间，含不含说不准；
 * - other：别的集、别的季、特典，或认不出集号。
 */
export type EpisodeFit = 'exact' | 'offset' | 'batch' | 'batch-unknown' | 'other'

const BATCH_TITLE = /合集|全\s*\d+\s*[集话話]|\bBatch\b|BD-?(?:Rip|Box|MV)|BDRip|\[\s*\d{1,3}\s*[-~]\s*\d{1,3}\s*(?:Fin|END)?\s*\]|\d{1,3}\s*-\s*\d{1,3}\s*(?:Fin|END)\b/i

/** 合集 / 整季 BD 包：后端按发布标题认出的（kind=batch）为准；没有集号又带 BD 字样的也按合集对待。 */
export function isBatchRelease(item: SearchItem): boolean {
  if (item.kind === 'batch') return true
  return typeof item.episode !== 'number' && BATCH_TITLE.test(item.title)
}

function covers(range: { low: number; high: number }, episode: number): boolean {
  return episode >= range.low && episode <= range.high
}

/** 首播日期往前留的余量：先行放送、时区差 */
const AIRING_SLACK_MS = 30 * 24 * 60 * 60 * 1000

/** 由作品首播日期（YYYY-MM-DD）算出 EpisodeTarget.notBefore；解析不了时不给 */
export function notBeforeFor(startDate: string | undefined): number | undefined {
  const start = startDate ? parsePublished(startDate) : null
  return start === null ? undefined : start - AIRING_SLACK_MS
}

export function episodeFit(item: SearchItem, target: EpisodeTarget): EpisodeFit {
  // 发布早于这一季开播：是前作的（续作页上搜回来的「- 01」「[01-28]」标题里常常没有季数）
  if (target.notBefore !== undefined && item.date) {
    const published = parsePublished(item.date)
    if (published !== null && published < target.notBefore) return 'other'
  }
  // 标题写明了别的季（搜「排球少年」会把四季的第 1 集都搜回来）；发布没写季数的不排除：
  // 很多字幕组给续作只写副标题。作品自己是第几季说不准时也不排除
  if (item.season !== undefined && target.season !== undefined && item.season !== target.season) return 'other'
  if (isBatchRelease(item)) {
    const range = item.episodeRange
    if (range === undefined) return 'batch-unknown'
    if (target.offset !== undefined && covers(range, target.episode + target.offset)) return 'batch'
    // 有前作、又知道本季几集时：区间伸出了本季的集数、却不含连续编号的这一集 —— 那是前作的
    // 整季包（续作页上的「[01-28]」）。不排除的话它会挂在第 1 集下面，点播放开的是前作第 1 集
    if (target.offset !== undefined && target.total !== undefined && range.high > target.total) return 'other'
    if (covers(range, target.episode)) return 'batch'
    return 'other'
  }
  if (typeof item.episode !== 'number') return 'other'
  // SP01、OVA2 不是正片的第 1、2 集（OVA、特别篇作品除外）
  if (!countsAsEpisode(item.kind, target)) return 'other'
  if (item.episode === target.episode) return 'exact'
  if (target.offset !== undefined && item.episode === target.episode + target.offset) return 'offset'
  return 'other'
}

const FIT_RANK: Record<EpisodeFit, number> = { exact: 0, offset: 0, batch: 1, 'batch-unknown': 2, other: 3 }

/** 排序用：单集在前，写明含这一集的合集其次，说不准的合集最后 */
export function fitRank(fit: EpisodeFit): number {
  return FIT_RANK[fit]
}

/** 这一集的发布（含可能含它的合集）；别的集、别的季、特典留给「显示全部发布」。 */
export function releasesForEpisode(items: SearchItem[], target: EpisodeTarget): SearchItem[] {
  return items.filter(item => episodeFit(item, target) !== 'other')
}

/**
 * 播放时交给后端选文件的集号：
 * - 单集发布按条目自己的集号（连续编号的「15」种子里的文件也叫 15）；
 * - 合集按用户选的那一集 —— 合集标题里的数字（「01-28」「全12集」）不是集号，后端也不再从它派生；
 *   跨季连续编号时同一集在合集里可能叫 episode+offset，两种都给，种子里只有一种时后端自动选中，
 *   两种都有（前作与本季装在一起）就弹选集。
 * 后端认不准时一律交给用户在选集弹窗里挑，不会自己猜。
 */
export function playbackHints(item: SearchItem, target: EpisodeTarget | null): Pick<TorrentPlayRequest, 'episodeHint' | 'altEpisodeHint'> {
  if (isBatchRelease(item)) {
    if (target === null) return {}
    const continuous = target.offset === undefined ? undefined : target.episode + target.offset
    const range = item.episodeRange
    if (continuous === undefined) return { episodeHint: target.episode }
    if (range !== undefined && covers(range, continuous) && !covers(range, target.episode)) return { episodeHint: continuous }
    if (range !== undefined && covers(range, target.episode) && !covers(range, continuous)) return { episodeHint: target.episode }
    return { episodeHint: target.episode, altEpisodeHint: continuous }
  }
  const episode = item.episode
  const playable = item.kind === 'movie' || countsAsEpisode(item.kind, target)
  if (typeof episode === 'number' && Number.isSafeInteger(episode) && episode > 0 && episode <= MAX_EPISODE && playable) {
    return { episodeHint: episode }
  }
  return {}
}

/**
 * 目录作品是第几季：标题里的「第二季 / S2 / II」等；没写就是 undefined（按第 1 季理解）。
 * 续作命名不统一（「TO THE TOP」这类不带数字）时解不出来。
 */
export function seasonOfTitle(title: string): number | undefined {
  const t = title.normalize('NFKC')
  const cn = /第\s*([一二三四五六七八九十\d]+)\s*[季期部]/.exec(t)
  if (cn) return chineseNumber(cn[1]!)
  const en = /(?:\bS|Season\s*)(\d{1,2})\b/i.exec(t)
  if (en) return Number(en[1])
  const roman = /\b(II|III|IV|V|VI)\b|\s(Ⅱ|Ⅲ|Ⅳ)\s*$/.exec(t)
  if (roman) return { II: 2, III: 3, IV: 4, V: 5, VI: 6, 'Ⅱ': 2, 'Ⅲ': 3, 'Ⅳ': 4 }[(roman[1] ?? roman[2])!]
  const nth = /(\d)(?:st|nd|rd|th)\s+Season/i.exec(t)
  if (nth) return Number(nth[1])
  return undefined
}

function chineseNumber(raw: string): number | undefined {
  if (/^\d+$/.test(raw)) return Number(raw)
  const digits: Record<string, number> = { 一: 1, 二: 2, 三: 3, 四: 4, 五: 5, 六: 6, 七: 7, 八: 8, 九: 9 }
  if (raw === '十') return 10
  if (raw.startsWith('十')) return 10 + (digits[raw[1]!] ?? 0)
  if (raw.endsWith('十')) return (digits[raw[0]!] ?? 0) * 10
  return digits[raw]
}

/** 作品身份：只在本机校验弹幕匹配没有认成别的作品（磁力不会发给 animego）。 */
export function catalogIdentity(media: Pick<MediaSummary, 'id' | 'title' | 'titleNative' | 'titleEnglish'>): Pick<TorrentPlayRequest, 'anilistId' | 'titles'> {
  const seen = new Set<string>()
  const titles: string[] = []
  for (const raw of [media.title, media.titleNative, media.titleEnglish]) {
    const title = raw?.trim()
    if (!title || seen.has(title.toLowerCase())) continue
    seen.add(title.toLowerCase())
    titles.push(title)
  }
  return { anilistId: media.id, titles }
}
