import { fetchDiscoverData, fetchMediaData } from './endpoints'
import type { CollectionEntry, CollectionStatus, SummaryMediaData } from './endpoints'
import type { MediaSummary } from '../components/media/types'
export { fetchCollection, saveCollection, deleteCollection } from './endpoints'
export type { CollectionData, CollectionEdit, CollectionEntry, CollectionStatus } from './endpoints'
export const COLLECTION_LABELS: Record<CollectionStatus, string> = { watching: '在看', plan_to_watch: '想看', completed: '看完', dropped: '弃番' }
export const entryStatus = (entry: CollectionEntry): CollectionStatus => entry.status
export function toSummary(media: SummaryMediaData): MediaSummary {
  return { id: media.anilistId, title: media.title, titleNative: media.titleNative, titleEnglish: media.titleEnglish,
    cover: media.cover, banner: media.banner, trailerId: media.trailerId, year: media.year, season: media.season,
    episodes: media.episodes, watched: 0, score: media.score, genres: media.genres, description: media.description,
    status: media.status, format: media.format, duration: media.duration, source: media.source, startDate: media.startDate,
    studios: media.studios, nextAiring: media.nextAiring, recentAiring: media.recentAiring,
    relations: media.relations?.map(r => ({ type: r.type, media: toSummary(r.media) })),
    recommendations: media.recommendations?.map(toSummary), characters: media.characters,
    episodeTitles: media.episodeTitles }
}
export type DiscoverSections = { key: string; title: string; items: MediaSummary[]; error?: string }[]
export async function fetchDiscover(signal?: AbortSignal): Promise<DiscoverSections> { return (await fetchDiscoverData(signal)).sections.map(section => ({ ...section, items: section.items.map(toSummary) })) }
export async function fetchMedia(id: number, signal?: AbortSignal): Promise<MediaSummary> { return toSummary(await fetchMediaData(id, signal)) }
export function collectionMedia(entry: CollectionEntry): MediaSummary { return { ...toSummary(entry.media), watched: entry.currentEpisode } }
