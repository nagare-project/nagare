import { useEffect, useState } from 'react'
import { fetchMedia } from '../../lib/media'
import { errorText } from '../../lib/format'
import type { MediaSummary } from './types'

export function useMediaDetails(id: number, enabled = true) {
  const [state, setState] = useState<{ id: number; media?: MediaSummary; loading: boolean; error?: string }>({ id, loading: enabled })
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    if (!enabled) return
    const abort = new AbortController()
    setState({ id, loading: true })
    void fetchMedia(id, abort.signal).then(media => { if (!abort.signal.aborted) setState({ id, media, loading: false }) })
      .catch(error => { if (!abort.signal.aborted) setState({ id, loading: false, error: errorText(error, '作品详情加载失败') }) })
    return () => abort.abort()
  }, [id, enabled, attempt])
  return { ...(state.id === id ? state : { id, loading: enabled }), retry: () => setAttempt(value => value + 1) }
}
