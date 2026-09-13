import { useEffect, useState } from 'react'
import { apiFetch } from '../../lib/api'
import { errorText } from '../../lib/format'
import type { EpisodeMetadata } from './types'

/** 图片独立加载；失败时保留作品资料和播放入口。 */
export function useEpisodeMetadata(id: number) {
  const [state, setState] = useState<{ id: number; episodes: EpisodeMetadata[]; error?: string }>({ id, episodes: [] })
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    const abort = new AbortController()
    setState({ id, episodes: [] })
    void apiFetch<EpisodeMetadata[]>(`/api/anime/${id}/episodes`, { signal: abort.signal })
      .then(episodes => { if (!abort.signal.aborted) setState({ id, episodes }) })
      .catch(error => { if (!abort.signal.aborted) setState({ id, episodes: [], error: errorText(error, '逐集图片加载失败') }) })
    return () => abort.abort()
  }, [id, attempt])
  return { ...(state.id === id ? state : { id, episodes: [] }), retry: () => setAttempt(n => n + 1) }
}
