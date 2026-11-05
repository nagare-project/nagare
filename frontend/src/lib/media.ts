import { fetchCatalogSearchData, fetchDiscoverData, fetchMediaData, fetchSeasonalData } from './endpoints'
import type { CatalogFreshness, CollectionEntry, CollectionStatus, SeasonName, SummaryMediaData } from './endpoints'
export type { SeasonName } from './endpoints'
import type { MediaSummary } from '../components/media/types'
export { fetchCollection, saveCollection, deleteCollection } from './endpoints'
export type { CollectionData, CollectionEdit, CollectionEntry, CollectionStatus } from './endpoints'
export const COLLECTION_LABELS: Record<CollectionStatus, string> = { watching: '在看', plan_to_watch: '想看', completed: '看完', dropped: '弃番' }
export const entryStatus = (entry: CollectionEntry): CollectionStatus => entry.status
export function toSummary(media: SummaryMediaData): MediaSummary {
  return { id: media.anilistId, title: media.title, titleNative: media.titleNative, titleRomaji: media.titleRomaji, titleEnglish: media.titleEnglish,
    cover: media.cover, banner: media.banner, trailerId: media.trailerId, year: media.year, season: media.season,
    episodes: media.episodes, watched: 0, score: media.score, genres: media.genres, description: media.description,
    status: media.status, format: media.format, duration: media.duration, source: media.source, startDate: media.startDate,
    studios: media.studios, nextAiring: media.nextAiring, recentAiring: media.recentAiring,
    relations: media.relations?.map(r => ({ type: r.type, media: toSummary(r.media) })),
    recommendations: media.recommendations?.map(toSummary), characters: media.characters,
    episodeTitles: media.episodeTitles, posterTones: media.posterTones, freshness: media.freshness }
}
export type DiscoverSections = { key: string; title: string; items: MediaSummary[]; error?: string }[]
/** 发现页：板块 + 这份数据的来历（离线 / 过期时才有，界面据此显示「离线 · 更新于 …」） */
export interface DiscoverView { sections: DiscoverSections; freshness?: CatalogFreshness }
export async function fetchDiscover(signal?: AbortSignal): Promise<DiscoverView> {
  const data = await fetchDiscoverData(signal)
  return { sections: data.sections.map(section => ({ ...section, items: section.items.map(toSummary) })), freshness: data.freshness }
}
export async function fetchMedia(id: number, signal?: AbortSignal): Promise<MediaSummary> { return toSummary(await fetchMediaData(id, signal)) }
export async function fetchCatalogSearch(q: string, signal?: AbortSignal): Promise<MediaSummary[]> { return (await fetchCatalogSearchData(q, signal)).items.map(toSummary) }
/** 季度页：这一季的作品 + 数据来历 */
export interface SeasonalView { items: MediaSummary[]; freshness?: CatalogFreshness }
export async function fetchSeasonal(season: SeasonName, year: number, signal?: AbortSignal): Promise<SeasonalView> {
  const data = await fetchSeasonalData(season, year, signal)
  return { items: data.items.map(toSummary), freshness: data.freshness }
}
export function collectionMedia(entry: CollectionEntry): MediaSummary { return { ...toSummary(entry.media), watched: entry.currentEpisode } }
