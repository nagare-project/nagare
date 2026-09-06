import { useCallback, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearch as useRouteSearch } from '@tanstack/react-router'
import { SearchBody } from '../components/search/SearchBody'
import { SearchForm } from '../components/search/SearchForm'
import type { PlayControl } from '../components/search/ResultRow'
import { useTorrentPlayback } from '../components/torrent/TorrentPlayContext'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useMagnetSearch } from '../hooks/useMagnetSearch'
import type { SearchState } from '../hooks/useMagnetSearch'
import { useSettings } from '../hooks/useSettings'
import type { SettingsState } from '../hooks/useSettings'
import { useSources } from '../hooks/useSources'
import type { SourcesState } from '../hooks/useSources'
import type { TorrentPlayState } from '../hooks/useTorrentPlay'
import type { SearchItem, SourceOutcome } from '../lib/endpoints'
import { errorText } from '../lib/format'
import { mono } from '../theme'
import '../components/search/search.css'

/** 状态行的一条消息 */
interface Notice {
  tone: 'dim' | 'ok' | 'err'
  text: string
}

/**
 * `/search?q=` 磁力搜索页（M2 搜索 + M3 边下边播）。
 * 关键词只活在 URL 里：表单提交 → navigate 改 q → useMagnetSearch 跟着 q 发请求，
 * 刷新 / 后退 / 分享链接都能复现同一次搜索。
 * 播放走 useTorrentPlay：底部状态条显示缓冲进展，需要选集时弹 EpisodePicker。
 */
export function SearchPage() {
  const { q } = useRouteSearch({ from: '/search' })
  const query = q ?? ''
  const navigate = useNavigate({ from: '/search' })

  // 焦点交接用的两个引用：选集弹窗关闭后要把焦点还回页面上一个有意义的位置
  const playTriggerRef = useRef<HTMLButtonElement | null>(null)
  const searchInputRef = useRef<HTMLInputElement | null>(null)

  const sources = useSources()
  const search = useMagnetSearch(query)
  // 设置只为读 torrent.enabled：引擎起不来时播放按钮要禁用并说明原因
  const settings = useSettings()
  const torrent = useTorrentPlayback()
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

  function handlePlay(item: SearchItem, trigger: HTMLButtonElement): void {
    setNotice(null)
    playTriggerRef.current = trigger
    torrent.play({ magnet: item.magnet, title: item.title }, item.title, restoreFocusAfterPicker)
  }

  /**
   * 选集弹窗关掉之后把焦点交还给谁。
   *
   * 首选是当初点下的那个播放按钮；但用户「选了一集」时它此刻正处于「启动中」的
   * 禁用态，而对禁用元素调 focus() 是静默失败 —— 焦点会留在 <body> 上，键盘用户
   * 得从页首重新 Tab。所以回落到搜索输入框：不是原位，但仍是页面上一个有意义的落点。
   */
  const restoreFocusAfterPicker = useCallback((): void => {
    const trigger = playTriggerRef.current
    if (trigger !== null && trigger.isConnected && !trigger.disabled) {
      trigger.focus()
      return
    }
    searchInputRef.current?.focus()
  }, [])

  if (sources.state.phase === 'unauthorized' || search.state.phase === 'unauthorized') {
    return <UnauthorizedNotice />
  }

  const play: PlayControl = {
    onPlay: handlePlay,
    busy: busyMagnet(torrent.state),
    engineDown: isEngineDown(settings.state),
  }
  const statusLine = notice ?? pickStatusLine(query, search.state, sources.state)

  return (
    <main className="search-shell">
      <header className="page-head">
        <h1 className="page-title">搜索</h1>
      </header>

      {play.engineDown && (
        <p className="alert-warn" role="alert">
          磁力引擎启动失败，边下边播不可用（搜索与复制磁力不受影响）。{' '}
          <Link to="/settings" hash="torrent" className="link alert-warn-link">
            去设置查看原因 →
          </Link>
        </p>
      )}

      {/* key=query：URL 里的关键词变了（后退 / 分享链接）就重置输入框 */}
      <SearchForm
        key={query}
        inputRef={searchInputRef}
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
        play={play}
      />

    </main>
  )
}

/** 规则 id → name 查找表（结果与状态条都只带 id） */
function sourceNames(state: SourcesState): Record<string, string> {
  if (state.phase !== 'ready') return {}
  return Object.fromEntries(state.data.sources.map((source) => [source.id, source.name]))
}

/** 正占着后端的那条磁力；选集与缓冲都算 pending（还没画面），streaming 才算 active */
function busyMagnet(state: TorrentPlayState): PlayControl['busy'] {
  switch (state.phase) {
    case 'starting':
    case 'selecting':
      return { magnet: state.magnet, stage: 'pending' }
    case 'streaming':
      return { magnet: state.magnet, stage: 'active' }
    default:
      return null
  }
}

/** 只有明确读到 enabled=false 才禁用：设置还没加载完时别把按钮锁死 */
function isEngineDown(state: SettingsState): boolean {
  return state.phase === 'ready' && !state.data.torrent.enabled
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
