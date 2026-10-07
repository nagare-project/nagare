import { useEffect, useState } from 'react'
import { fetchCatalogSearch } from '../../lib/media'
import { errorText } from '../../lib/format'
import { Icon } from '../ui/Icon'
import { DiscoverCard } from './DiscoverCard'
import type { MediaSummary } from './types'

/** 与后端 maxSearchRunes 一致：再长的关键词后端会拒绝 */
const MAX_QUERY_LENGTH = 64

/**
 * 发现页顶部的作品搜索框。只在提交时搜：上游 animego 的搜索端点有限速且会打到 AniList，
 * 边打字边搜会把配额烧光（后端另有缓存与节流兜底）。
 */
export function CatalogSearchForm({ query, onSearch }: { query: string; onSearch: (q: string) => void }) {
  const [text, setText] = useState(query)
  // 地址栏变了（后退 / 前进 / 点了「清除」）时同步输入框
  useEffect(() => setText(query), [query])

  return (
    <form
      className="catalog-search"
      role="search"
      onSubmit={(event) => {
        event.preventDefault()
        onSearch(text.trim())
      }}
    >
      <span className="catalog-search-icon" aria-hidden="true">
        <Icon name="search" size={18} />
      </span>
      <input
        className="input catalog-search-input"
        type="search"
        value={text}
        onChange={(event) => setText(event.target.value)}
        placeholder="搜索作品名，如：葬送的芙莉莲"
        aria-label="搜索作品名"
        maxLength={MAX_QUERY_LENGTH}
        enterKeyHint="search"
        autoComplete="off"
      />
      <button type="submit" className="btn btn--sm btn--primary">
        搜索
      </button>
      {query !== '' && (
        <button type="button" className="btn btn--sm" onClick={() => onSearch('')}>
          清除
        </button>
      )}
    </form>
  )
}

type SearchState =
  | { phase: 'loading' }
  | { phase: 'ready'; items: MediaSummary[] }
  | { phase: 'error'; message: string }

/** 搜索结果：与季度页同一种作品卡网格，点进作品页再选集播放 */
export function CatalogSearchResults({ query }: { query: string }) {
  const [state, setState] = useState<SearchState>({ phase: 'loading' })
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const abort = new AbortController()
    setState({ phase: 'loading' })
    fetchCatalogSearch(query, abort.signal)
      .then((items) => {
        if (!abort.signal.aborted) setState({ phase: 'ready', items })
      })
      .catch((err: unknown) => {
        if (abort.signal.aborted) return
        console.error('搜索作品失败', err)
        setState({ phase: 'error', message: errorText(err, '搜索失败') })
      })
    return () => abort.abort()
  }, [query, attempt])

  return (
    <section className="catalog-search-results" aria-label={`「${query}」的搜索结果`} aria-busy={state.phase === 'loading'}>
      <h2 className="row-title">搜索「{query}」</h2>
      {state.phase === 'loading' && (
        <p className="result result--dim" role="status">
          正在搜索 …
        </p>
      )}
      {state.phase === 'error' && (
        <p className="result result--err" role="alert">
          {state.message}{' '}
          <button type="button" className="btn btn--sm" onClick={() => setAttempt((n) => n + 1)}>
            重试
          </button>
        </p>
      )}
      {state.phase === 'ready' && state.items.length === 0 && (
        <p className="result result--dim" role="status">
          没有找到「{query}」相关的作品。试试原名、英文名或别的叫法。
        </p>
      )}
      {state.phase === 'ready' && state.items.length > 0 && (
        <ul className="poster-grid">
          {state.items.map((media) => (
            <DiscoverCard key={media.id} media={media} />
          ))}
        </ul>
      )}
    </section>
  )
}
