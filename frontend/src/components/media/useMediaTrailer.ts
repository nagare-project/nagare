import { useMediaDetails } from './useMediaDetails'
import type { MediaSummary } from './types'

/** 旧上游缓存还没补齐 trailer 时按需读详情；保持由 animego 提供元数据。 */
export function useMediaTrailer(media: MediaSummary | undefined, active: boolean) {
  const detail = useMediaDetails(media?.id ?? 0, active && !!media?.id && !media.trailerId)
  return media?.trailerId ?? detail.media?.trailerId
}
