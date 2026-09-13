import type { EpisodeMetadata, MediaSummary } from './types'

/** 优先使用精确播出时间；日期不足时由作品放送信息判断。 */
export function episodeHasAired(episode: EpisodeMetadata, media: MediaSummary, now = Date.now()): boolean {
  const at = Date.parse(episode.airedAt || episode.airDate || '')
  if (Number.isFinite(at)) return at <= now
  if (media.status === 'NOT_YET_RELEASED') return false
  if (media.nextAiring && media.nextAiring.at * 1000 > now) return episode.episode < media.nextAiring.episode
  return true
}

export function nextEpisodeAiring(media: MediaSummary, episodes: EpisodeMetadata[], now = Date.now()) {
  if (media.nextAiring && media.nextAiring.at * 1000 > now) return media.nextAiring
  const next = episodes.map(episode => ({ episode: episode.episode, at: Date.parse(episode.airedAt || '') / 1000 }))
    .filter(episode => Number.isFinite(episode.at) && episode.at * 1000 > now).sort((a, b) => a.at - b.at)[0]
  return next
}
