import type { SearchItem, TorrentPlayRequest } from '../../lib/endpoints'
import type { MediaSummary } from './types'

/**
 * 磁力发布的集号与合集。
 *
 * 发布标题里的集号是字幕组自己的编号（跨季连续编号时第二季第 3 集叫 15）。集号、合集区间都由
 * 后端按同一条解析链给出（SearchItem.episode / kind / episodeRange）；唯一的例外是后端没认出合集、
 * 又解不出集号的发布，按标题里的 BD 一类字样兜底当合集看（isBatchRelease），只影响展示与排序。
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

/**
 * 下一集没看的；全看完了停在最后一集；不知道播到哪一集就从下一集算起。
 * 只给只会按集号搜的老插件用（新插件按作品搜回全部发布，用不上集号）。
 */
export function defaultTorrentEpisode(media: MediaSummary): number {
  const next = Math.max(1, media.watched + 1)
  const aired = airedEpisodeCount(media)
  return aired > 0 ? Math.min(next, aired) : next
}

/** 集号上限，与后端一致（长篇连载过千集，再大就不是集号了） */
const MAX_EPISODE = 9999

const KINDS_BY_FORMAT: Record<string, readonly string[]> = { OVA: ['ova', 'sp'], SPECIAL: ['sp', 'ova'] }

/** 作品形式决定哪些类型也算正片（只有 OVA、特别篇需要：它们自己的发布本来就标着 OVA、SP） */
export function extraKindsFor(format: string | undefined): readonly string[] | undefined {
  return format === undefined ? undefined : KINDS_BY_FORMAT[format]
}

const BATCH_TITLE = /合集|全\s*\d+\s*[集话話]|\bBatch\b|BD-?(?:Rip|Box|MV)|BDRip|\[\s*\d{1,3}\s*[-~]\s*\d{1,3}\s*(?:Fin|END)?\s*\]|\d{1,3}\s*-\s*\d{1,3}\s*(?:Fin|END)\b/i

/** 合集 / 整季 BD 包：后端按发布标题认出的（kind=batch）为准；没有集号又带 BD 字样的也按合集对待。 */
export function isBatchRelease(item: SearchItem): boolean {
  if (item.kind === 'batch') return true
  return typeof item.episode !== 'number' && BATCH_TITLE.test(item.title)
}

/**
 * 播放时交给后端选文件的集号：单集发布按条目自己的集号（连续编号的「15」种子里的文件也叫 15）。
 * 合集不给 —— 合集标题里的数字（「01-28」「全12集」）不是集号，播放前由用户在文件列表里挑；
 * SP01、OVA2 也不当正片的第 1、2 集（extraKinds 列出的类型除外）。后端认不准时一律弹文件列表，不会自己猜。
 */
export function playbackHints(item: SearchItem, extraKinds?: readonly string[]): Pick<TorrentPlayRequest, 'episodeHint'> {
  if (isBatchRelease(item)) return {}
  const episode = item.episode
  const kind = item.kind
  const playable = kind === undefined || kind === 'main' || kind === 'movie' || (extraKinds?.includes(kind) ?? false)
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
export function catalogIdentity(media: Pick<MediaSummary, 'id' | 'title' | 'titleRomaji' | 'titleNative' | 'titleEnglish'>): Pick<TorrentPlayRequest, 'anilistId' | 'titles'> {
  const seen = new Set<string>()
  const titles: string[] = []
  for (const raw of [media.title, media.titleRomaji, media.titleNative, media.titleEnglish]) {
    const title = raw?.trim()
    if (!title || seen.has(title.toLowerCase())) continue
    seen.add(title.toLowerCase())
    titles.push(title)
  }
  return { anilistId: media.id, titles }
}
