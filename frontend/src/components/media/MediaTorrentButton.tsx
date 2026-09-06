import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useTorrentPlayback } from '../torrent/TorrentPlayContext'
import { fetchSettings, fetchSources, searchMagnets } from '../../lib/endpoints'
import type { SearchItem, SearchResult, SourceOutcome, SourcesData } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { Icon } from '../ui/Icon'
import type { MediaSummary } from './types'
import './media-play.css'

const MAX_EPISODE_BUTTONS = 120
const MAX_RESOURCE_RESULTS = 80

type ResourceState =
  | { phase: 'idle' }
  | { phase: 'searching'; episode: number }
  | { phase: 'ready'; episode: number; result: SearchResult; sources: SourcesData; engineDown: boolean }
  | { phase: 'error'; episode: number; message: string }

/** 只列已经播出的集。未知总集数时依然可以依靠最近/下次放送信息给出已有集数。 */
export function airedEpisodeCount(media: MediaSummary): number {
  if (media.format === 'MOVIE') return 1
  const knownAired = Math.max(
    media.watched,
    media.recentAiring?.episode ?? 0,
    (media.nextAiring?.episode ?? 1) - 1,
  )
  if (media.status === 'NOT_YET_RELEASED') return knownAired
  if (media.status === 'RELEASING' || media.episodes === null) return knownAired
  return Math.max(knownAired, media.episodes ?? 0)
}

function episodeNumbers(media: MediaSummary): number[] {
  const count = airedEpisodeCount(media)
  if (count <= MAX_EPISODE_BUTTONS) return Array.from({ length: count }, (_, index) => index + 1)
  const start = Math.max(1, Math.min(count - MAX_EPISODE_BUTTONS + 1, media.watched - 10))
  return Array.from({ length: MAX_EPISODE_BUTTONS }, (_, index) => start + index)
}

/**
 * 目录作品的磁力入口：用户先选目标集，再用本机规则找资源，最后显式选择版本。
 * 作品 ID 不参与找源；它只提供名称与集数上下文，磁力始终来自用户配置的本机规则。
 */
export function MediaTorrentButton({ media, onOpenChange }: { media: MediaSummary; onOpenChange?: (open: boolean) => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const requestVersion = useRef(0)
  const torrent = useTorrentPlayback()
  const [query, setQuery] = useState(media.title)
  const [manualEpisode, setManualEpisode] = useState(String(Math.max(1, media.watched + 1)))
  const [state, setState] = useState<ResourceState>({ phase: 'idle' })
  const numbers = episodeNumbers(media)
  const titleByEpisode = new Map(media.episodeTitles?.map(item => [item.episode, item.title]) ?? [])

  useEffect(() => () => { requestVersion.current += 1 }, [])
  useEffect(() => {
    setQuery(media.title)
    setManualEpisode(String(Math.max(1, media.watched + 1)))
    setState({ phase: 'idle' })
  }, [media.id, media.title, media.watched])

  async function findEpisode(episode: number): Promise<void> {
    if (!Number.isSafeInteger(episode) || episode < 1) return
    const searchQuery = query.trim() || media.title
    const version = ++requestVersion.current
    setState({ phase: 'searching', episode })
    try {
      const [result, sources, settings] = await Promise.all([
        searchMagnets(searchQuery),
        fetchSources(),
        fetchSettings(),
      ])
      if (version !== requestVersion.current) return
      setState({ phase: 'ready', episode, result, sources, engineDown: !settings.torrent.enabled })
    } catch (err) {
      if (version === requestVersion.current) setState({ phase: 'error', episode, message: errorText(err, '搜索磁力资源失败') })
    }
  }

  function submitManual(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    void findEpisode(Number(manualEpisode))
  }

  function start(item: SearchItem, episode: number, button: HTMLButtonElement): void {
    torrent.play(
      { magnet: item.magnet, title: item.title, episodeHint: episode },
      item.title,
      () => {
        if (button.isConnected && !button.disabled) button.focus()
        else trigger.current?.focus()
      },
    )
    dialog.current?.close()
  }

  const selectedEpisode = state.phase === 'idle' ? null : state.episode
  return <>
    <button type="button" className="discover-card-play discover-card-torrent" ref={trigger}
      aria-label={`选择集数并搜索磁力：${media.title}`}
      onClick={() => { dialog.current?.showModal(); onOpenChange?.(true) }}>
      <Icon name="download" size={18} />选集
    </button>
    <dialog ref={dialog} className="media-play-dialog media-torrent-dialog" aria-label={`${media.title} 磁力选集`}
      onClose={() => {
        requestVersion.current += 1
        trigger.current?.focus()
        onOpenChange?.(false)
      }}
      onClick={event => { if (event.target === dialog.current) dialog.current.close() }}>
      <div className="media-play-body">
        <button type="button" className="icon-button media-play-close" aria-label="关闭磁力选集" onClick={() => dialog.current?.close()}><Icon name="close" /></button>
        <p className="media-play-kicker">磁力边下边播</p>
        <h2>{media.title}</h2>
        <p className="media-play-hint">选择要看的集数，再从本机规则返回的资源中选一个版本。开始后可关闭窗口或切换页面，底部会持续显示缓冲状态。</p>

        <label className="media-torrent-query">
          <span>搜索词</span>
          <input className="input" type="search" value={query} onChange={event => setQuery(event.target.value)} spellCheck={false} />
        </label>

        {numbers.length > 0 && <section className="media-episode-section" aria-labelledby={`media-episodes-${media.id}`}>
          <div className="media-play-selection">
            <h3 id={`media-episodes-${media.id}`}>{media.format === 'MOVIE' ? '影片' : '选择集数'}</h3>
            <span className="result result--dim">已播出 {airedEpisodeCount(media)} 集</span>
          </div>
          <div className="media-episode-grid">
            {numbers.map(episode => <button type="button" key={episode}
              className={selectedEpisode === episode ? 'media-episode-button media-episode-button--selected' : 'media-episode-button'}
              title={titleByEpisode.get(episode)} aria-label={`搜索第 ${episode} 集资源`}
              onClick={() => { setManualEpisode(String(episode)); void findEpisode(episode) }}>
              <strong>{media.format === 'MOVIE' ? '播放' : episode}</strong>
              {titleByEpisode.has(episode) && <span>{titleByEpisode.get(episode)}</span>}
            </button>)}
          </div>
          {airedEpisodeCount(media) > numbers.length && <p className="media-play-hint">这里只显示靠近当前进度的 {numbers.length} 集，其他集可在下方输入集号。</p>}
        </section>}

        <form className="media-manual-episode" onSubmit={submitManual}>
          <label><span>{numbers.length ? '指定其他集' : '目标集数'}</span><input className="input" type="number" min="1" step="1" required value={manualEpisode} onChange={event => setManualEpisode(event.target.value)} /></label>
          <button type="submit" className="btn btn--primary" disabled={state.phase === 'searching'}>{state.phase === 'searching' ? '搜索中…' : '查找资源'}</button>
        </form>

        {state.phase === 'searching' && <p className="result result--dim" role="status">正在搜索第 {state.episode} 集资源…</p>}
        {state.phase === 'error' && <p className="result result--err" role="alert">{state.message} <button type="button" className="link" onClick={() => void findEpisode(state.episode)}>重试</button></p>}
        {state.phase === 'ready' && <ResourceResults state={state} busy={torrent.busy} onPlay={start} />}
      </div>
    </dialog>
  </>
}

function ResourceResults({ state, busy, onPlay }: {
  state: Extract<ResourceState, { phase: 'ready' }>
  busy: boolean
  onPlay: (item: SearchItem, episode: number, button: HTMLButtonElement) => void
}) {
  if (state.sources.sources.length === 0) return <div className="media-resource-empty">
    <p className="result result--warn">尚未配置资源源。</p>
    <a className="btn btn--sm" href="/settings#sources">去设置添加规则来源</a>
  </div>

  const trouble = state.result.sources.filter(outcome => outcome.state === 'dead' || outcome.state === 'failed')
  const sourceNames = new Map(state.sources.sources.map(source => [source.id, source.name]))
  return <section className="media-resource-results" aria-label={`第 ${state.episode} 集磁力资源`}>
    <div className="media-play-selection"><h3>第 {state.episode} 集资源</h3><span className="result result--dim">{state.result.items.length} 条</span></div>
    {state.engineDown && <p className="result result--err" role="alert">磁力引擎不可用。<a className="link" href="/settings#torrent">查看设置</a></p>}
    {trouble.length > 0 && <p className="result result--warn" role="status">{sourceTrouble(trouble)}</p>}
    {state.result.items.length === 0 ? <p className="media-play-hint">启用的源没有返回结果。可以修改搜索词后重试；源异常不等于这部作品没有资源。</p> :
      <ul className="media-resource-list">{state.result.items.slice(0, MAX_RESOURCE_RESULTS).map((item, index) => <li key={`${item.source}-${index}`}>
        <div><strong>{item.title}</strong><span>{[item.fansub, item.size, typeof item.seeders === 'number' ? `做种 ${item.seeders}` : null, sourceNames.get(item.source) ?? item.source].filter(Boolean).join(' · ')}</span></div>
        <button type="button" className="btn btn--sm btn--primary" disabled={busy || state.engineDown}
          title={busy ? '已有磁力任务，请先停止底部状态条中的任务' : undefined}
          onClick={event => onPlay(item, state.episode, event.currentTarget)}>播放</button>
      </li>)}</ul>}
    {state.result.items.length > MAX_RESOURCE_RESULTS && <p className="media-play-hint">只显示前 {MAX_RESOURCE_RESULTS} 条，请收窄搜索词。</p>}
  </section>
}

function sourceTrouble(outcomes: SourceOutcome[]): string {
  const dead = outcomes.filter(outcome => outcome.state === 'dead').length
  const failed = outcomes.filter(outcome => outcome.state === 'failed').length
  return [dead ? `${dead} 个源规则异常` : '', failed ? `${failed} 个源连接失败` : ''].filter(Boolean).join('，')
}
