import { apiFetch } from './api'
import type { MediaSummary } from '../components/media/types'

export type DiscoverSections = Record<string, MediaSummary[]>
export type CollectionStatus = 'watching' | 'plan_to_watch' | 'completed' | 'paused' | 'dropped'
export const COLLECTION_LABELS: Record<CollectionStatus, string> = { watching: '在看', plan_to_watch: '想看', completed: '看完', paused: '搁置', dropped: '弃番' }
export interface CollectionEntry {
  anilistId: number; status: Exclude<CollectionStatus, 'paused'>; currentEpisode: number; score: number | null
  paused: boolean; startedAt: string; completedAt: string; repeat: number
  titleRomaji: string; titleChinese?: string; titleNative?: string; coverImageUrl?: string; bannerImageUrl?: string
  episodes: number | null; season?: string; seasonYear?: number; animeStatus?: string
}
export interface CollectionData { loggedIn: boolean; entries: CollectionEntry[] }
export interface CollectionEdit { status: CollectionStatus; progress: number; score: number | null; startedAt: string; completedAt: string; repeat: number }
export const entryStatus = (entry: CollectionEntry): CollectionStatus => entry.paused && entry.status === 'watching' ? 'paused' : entry.status
export const fetchDiscover = (section = '', genre = '', signal?: AbortSignal) => apiFetch<DiscoverSections>(`/api/catalog/discover?section=${encodeURIComponent(section)}&genre=${encodeURIComponent(genre)}`, { signal })
export const fetchMedia = (id: number, signal?: AbortSignal) => apiFetch<MediaSummary>(`/api/catalog/anime/${id}`, { signal })
export const fetchCollection = () => apiFetch<CollectionData>('/api/catalog/list')
export const saveCollection = (id: number, data: CollectionEdit) => apiFetch(`/api/catalog/list/${id}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) })
export const deleteCollection = (id: number) => apiFetch(`/api/catalog/list/${id}`, { method: 'DELETE' })
export function collectionMedia(entry: CollectionEntry): MediaSummary {
  return { id: entry.anilistId, title: entry.titleRomaji || entry.titleChinese || String(entry.anilistId), titleNative: entry.titleNative,
    cover: entry.coverImageUrl, banner: entry.bannerImageUrl, year: entry.seasonYear,
    season: ({ WINTER: '冬', SPRING: '春', SUMMER: '夏', FALL: '秋' } as Record<string, string>)[entry.season ?? ''],
    episodes: entry.episodes, watched: entry.currentEpisode, status: entry.animeStatus, genres: [] }
}
