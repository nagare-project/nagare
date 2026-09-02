import { useMemo, useState } from 'react'
import { Link, useNavigate, useSearch as useRouteSearch } from '@tanstack/react-router'
import { SearchBody } from '../components/search/SearchBody'
import { SearchForm } from '../components/search/SearchForm'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useMagnetSearch } from '../hooks/useMagnetSearch'
import type { SearchState } from '../hooks/useMagnetSearch'
import { useSources } from '../hooks/useSources'
import type { SourcesState } from '../hooks/useSources'
import type { SourceOutcome } from '../lib/endpoints'
import { errorText } from '../lib/format'
import { hudPalette } from '../lib/palette'
import { label, mono } from '../tokens'
import '../components/search/search.css'

/** 状态行的一条消息 */
interface Notice {
  tone: 'dim' | 'ok' | 'err'
  text: string
}

/**
 * `/search?q=` 磁力搜索页（M2）。
 * 关键词只活在 URL 里：表单提交 → navigate 改 q → useMagnetSearch 跟着 q 发请求，
 * 刷新 / 后退 / 分享链接都能复现同一次搜索。
 */
export function SearchPage() {
  const { q } = useRouteSearch({ from: '/search' })
  const query = q ?? ''
  const navigate = useNavigate({ from: '/search' })

  const sources = useSources()
  const search = useMagnetSearch(query)
  const [notice, setNotice] = useState<Notice | null>(null)
  const [togglingId, setTogglingId] = useState<string | null>(null)

  const names = useMemo(() => sourceNames(sources.state), [sources.state])

  function handleSubmit(next: string): void {
    setNotice(null)
    void navigate({ to: '/search', search: { q: next === '' ? undefined : next } })
  }

  async function handleToggleSource(id: string, enabled: boolean): Promise<void> {
    if (togglingId !== null) return
    setTogglingId(id)
    setNotice(null)
    try {
      await sources.setEnabled(id, enabled)
      await search.refetch()
    } catch (err) {
      console.error('切换源启用状态失败', err)
      setNotice({ tone: 'err', text: errorText(err, '切换源启用状态失败') })
    } finally {
      setTogglingId(null)
    }
  }

  if (sources.state.phase === 'unauthorized' || search.state.phase === 'unauthorized') {
    return <UnauthorizedNotice />
  }

  const statusLine = notice ?? pickStatusLine(query, search.state, sources.state)

  return (
    <main className="search-shell" style={hudPalette}>
      <header className="topbar">
        <h1 className="topbar-brand">
          nagare
          <span className="topbar-kana" aria-hidden="true">
            流れ
          </span>
        </h1>
        <span className="topbar-section" style={label}>
          magnet search
        </span>
        <span className="topbar-spacer" />
        <nav className="topbar-actions" aria-label="页面导航">
          <Link to="/" className="hud-link topbar-link">
            媒体库
          </Link>
          <Link to="/settings" className="hud-link topbar-link">
            设置
          </Link>
        </nav>
      </header>

      {/* key=query：URL 里的关键词变了（后退 / 分享链接）就重置输入框 */}
      <SearchForm
        key={query}
        initialQuery={query}
        onSubmit={handleSubmit}
        busy={search.state.phase === 'searching'}
        autoFocus={query === ''}
      />

      <p
        className={statusLine === null ? 'result search-status' : `result search-status result--${statusLine.tone}`}
        style={mono}
        role="status"
        aria-live="polite"
      >
        {statusLine?.text}
      </p>

      <SearchBody
        query={query}
        sources={sources.state}
        search={search.state}
        names={names}
        onRetrySources={() => void sources.reload()}
        onRetrySearch={() => void search.refetch()}
        onToggleSource={(id, enabled) => void handleToggleSource(id, enabled)}
        toggling={togglingId !== null}
      />
    </main>
  )
}

/** 规则 id → name 查找表（结果与状态条都只带 id） */
function sourceNames(state: SourcesState): Record<string, string> {
  if (state.phase !== 'ready') return {}
  return Object.fromEntries(state.data.sources.map((source) => [source.id, source.name]))
}

/** 状态行：搜索进度 > 结果摘要 > 规则摘要 */
function pickStatusLine(query: string, search: SearchState, sources: SourcesState): Notice | null {
  if (query !== '' && search.phase === 'searching') {
    return { tone: 'dim', text: `正在搜索「${query}」…` }
  }
  if (search.phase === 'ready') {
    return { tone: 'dim', text: summarize(search.data.items.length, search.data.sources) }
  }
  if (sources.phase === 'ready' && sources.data.sources.length > 0) {
    const enabled = sources.data.sources.filter((source) => source.enabled).length
    return { tone: 'dim', text: `${sources.data.sources.length} 个源 · ${enabled} 个启用` }
  }
  return null
}

/** `12 条 · 3 个源正常 · 1 个源异常 · 1 个连接失败` */
function summarize(itemCount: number, outcomes: SourceOutcome[]): string {
  const count = (state: SourceOutcome['state']) =>
    outcomes.filter((outcome) => outcome.state === state).length
  const parts = [`${itemCount} 条`, `${count('ok')} 个源正常`]
  if (count('zero') > 0) parts.push(`${count('zero')} 个无结果`)
  if (count('dead') > 0) parts.push(`${count('dead')} 个源异常`)
  if (count('failed') > 0) parts.push(`${count('failed')} 个连接失败`)
  if (count('disabled') > 0) parts.push(`${count('disabled')} 个已禁用`)
  return parts.join(' · ')
}
