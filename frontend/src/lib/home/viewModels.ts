// 接口数据（GET /api/discover）→ 发现页各板块要画的小结构。
//
// 每张卡显示什么都在这里决定，组件只拿几段现成的字。完结佳作「换一批」在浏览器里也走 gemCard，
// 所以这个模块保持纯函数：不碰 React、不碰 DOM。

import type { HomeAiringData, HomeMediaData, RankedMediaData } from '../endpoints'
import type { Airing } from './heroStatus'
import { formatLabel, genreLabel, seasonLabel, seasonYearLabel, statusLabel } from './labels'
import { titleTokens } from './titleTokens'

const SYNOPSIS_CHARS = 160
const HERO_GENRES = 3
const POPOVER_GENRES = 3

/** AniList 评分（0–100）写成「8.5」；没有时为 null */
export function scoreText(averageScore: number | null | undefined): string | null {
  return averageScore ? (averageScore / 10).toFixed(1) : null
}

/** Bangumi 评分写成一位小数：先按十分位取整 —— 7.85 存下来是 7.8499…，toFixed(1) 会给 7.8，对照 Bangumi 的人期望看到 7.9 */
export function bangumiScoreText(score: number | null | undefined): string | null {
  return score ? (Math.round(score * 10) / 10).toFixed(1) : null
}

export function episodesText(n: number | null | undefined): string | null {
  if (!n || n <= 0) return null
  return `${n} 集`
}

function joinMeta(parts: Array<string | null | undefined>): string {
  return parts.filter((p): p is string => !!p).join(' · ')
}

function seasonText(row: HomeMediaData): string | null {
  return row.season && row.year ? seasonYearLabel(row.season, row.year) : null
}

function localGenres(row: HomeMediaData, max: number): string[] {
  return row.genres.slice(0, max).map(genreLabel)
}

/** 截到 len 个字，截了就补「...」 */
function truncate(text: string | undefined, len: number): string {
  if (!text) return ''
  return text.length > len ? `${text.slice(0, len)}...` : text
}

export interface CardBase {
  id: number
  title: string
  cover: string | null
  /** OKLCH 色相；null = 中性 */
  hue: number | null
}

function base(row: HomeMediaData): CardBase {
  return { id: row.anilistId, title: row.title, cover: row.cover ?? null, hue: row.hue }
}

/** 某部作品在放送表里的各集（毫秒，先后排好） */
export function airingsOf(schedule: readonly HomeAiringData[], id: number): Airing[] {
  return schedule
    .filter((s) => s.anilistId === id)
    .map((s) => ({ at: s.airingAt * 1000, ep: s.episode }))
    .sort((a, b) => a.at - b.at)
}

// ── 横幅 ────────────────────────────────────────────────────────────────────

export interface HeroSlide extends CardBase {
  tokens: string[]
  /** 横幅图；没有时用封面顶替 */
  banner: string | null
  hasBanner: boolean
  genres: string[]
  score: string | null
  status: string | null
  statusLabel: string
  episodes: number | null
  /** 这部作品在放送表近七日里的各集，毫秒，先后排好 */
  airings: Airing[]
  synopsis: string
}

export function heroSlide(row: HomeMediaData, schedule: readonly HomeAiringData[]): HeroSlide {
  const b = base(row)
  return {
    ...b,
    tokens: titleTokens(b.title),
    banner: row.banner || row.cover || null,
    hasBanner: !!row.banner,
    genres: localGenres(row, HERO_GENRES),
    score: scoreText(row.score),
    status: row.status ?? null,
    statusLabel: row.status ? statusLabel(row.status) : '',
    episodes: row.episodes,
    airings: airingsOf(schedule, row.anilistId),
    synopsis: truncate(row.description, SYNOPSIS_CHARS),
  }
}

// ── 本季高分 ────────────────────────────────────────────────────────────────

export interface SeasonCard extends CardBase {
  score: string | null
  /** 「TV · 连载中」 */
  meta: string
  statusLabel: string
  /** 浮层里的「TV · 12 集 · 2026 秋季」 */
  popMeta: string
  bangumi: string | null
  genres: string[]
  /** 这部作品在放送表近七日里的各集（毫秒，先后排好）；浮层按时钟从里面挑下一集 */
  airings: Airing[]
}

export function seasonCard(row: HomeMediaData, airings: Airing[]): SeasonCard {
  const format = row.format ? formatLabel(row.format) : null
  const status = row.status ? statusLabel(row.status) : ''
  return {
    ...base(row),
    score: scoreText(row.score),
    meta: joinMeta([format, status]),
    statusLabel: status,
    popMeta: joinMeta([format, row.format === 'MOVIE' ? null : episodesText(row.episodes), seasonText(row)]),
    bangumi: bangumiScoreText(row.bangumiScore),
    genres: localGenres(row, POPOVER_GENRES),
    airings,
  }
}

// ── 按色调逛 ────────────────────────────────────────────────────────────────

export interface HueCard extends CardBase {
  score: string | null
  meta: string
}

export function hueCard(row: HomeMediaData): HueCard {
  return {
    ...base(row),
    score: scoreText(row.score),
    meta: joinMeta([row.format ? formatLabel(row.format) : null, row.status ? statusLabel(row.status) : null]),
  }
}

// ── 今日更新 ────────────────────────────────────────────────────────────────

export interface TodayCard extends CardBase {
  /** 同一部作品一天可能播两集，按这个区分 */
  key: number
  /** 毫秒 */
  at: number
  ep: number
}

export function todayCard(row: HomeAiringData): TodayCard {
  return {
    id: row.anilistId,
    title: row.title,
    cover: row.cover ?? null,
    hue: row.hue,
    key: row.scheduleId || row.anilistId * 1000 + row.episode,
    at: row.airingAt * 1000,
    ep: row.episode,
  }
}

// ── 大家都在追 ──────────────────────────────────────────────────────────────

export interface TrendCard extends CardBase {
  rank: number
  watchers: number
  /** 「AniList 8.5」，否则「Bangumi 7.9」，否则空串 */
  scoreLine: string
}

export function trendCard(row: RankedMediaData): TrendCard {
  const anilist = scoreText(row.score)
  const bangumi = bangumiScoreText(row.bangumiScore)
  return {
    ...base(row),
    rank: row.rank,
    watchers: row.watchers,
    scoreLine: anilist ? `AniList ${anilist}` : bangumi ? `Bangumi ${bangumi}` : '',
  }
}

// ── 完结佳作 ────────────────────────────────────────────────────────────────

export interface GemCard extends CardBase {
  native: string | null
  meta: string
  score: string | null
  scoreSource: 'Bangumi' | 'AniList' | null
}

export function gemCard(row: HomeMediaData): GemCard {
  const format = row.format ? formatLabel(row.format) : null
  const count = row.format === 'MOVIE' ? null : episodesText(row.episodes)
  const bangumi = bangumiScoreText(row.bangumiScore)
  const anilist = scoreText(row.score)
  const b = base(row)
  return {
    ...b,
    // 显示标题下面的原名 —— 两者一样时就不重复
    native: row.titleNative && row.titleNative !== b.title ? row.titleNative : null,
    meta: joinMeta([seasonText(row), format && count ? `${format} · ${count}` : (format ?? count)]),
    score: bangumi ?? anilist,
    scoreSource: bangumi ? 'Bangumi' : anilist ? 'AniList' : null,
  }
}

// ── 年度榜 ──────────────────────────────────────────────────────────────────

export interface YearCard extends CardBase {
  rank: number
  /** 只写季度 —— 年份在板块标题里 */
  season: string
  score: string | null
}

export function yearCard(row: HomeMediaData, rank: number): YearCard {
  return {
    ...base(row),
    rank,
    season: row.season ? seasonLabel(row.season) : '',
    score: scoreText(row.score),
  }
}
