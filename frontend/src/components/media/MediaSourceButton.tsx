import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useTorrentPlayback } from '../torrent/TorrentPlayContext'
import {
  fetchSourcePlugin,
  playSourceCandidate,
  streamSourceCandidates,
} from '../../lib/endpoints'
import type {
  PluginSource,
  SourceCandidate,
  SourcePluginView,
  SourceResolveRequest,
} from '../../lib/endpoints'
import { errorText, formatBytes } from '../../lib/format'
import { Icon } from '../ui/Icon'
import { airedEpisodeCount } from './MediaTorrentButton'
import type { MediaSummary } from './types'
import './media-play.css'

const MAX_EPISODE_BUTTONS = 120
const MAX_CANDIDATES = 100

interface CandidateProgress {
  episode: number
  plugin: SourcePluginView
  candidates: SourceCandidate[]
  errors: string[]
}

type CandidateState =
  | { phase: 'idle' }
  | ({ phase: 'searching' | 'ready' } & CandidateProgress)
  | { phase: 'error'; episode: number; message: string }

function episodeNumbers(media: MediaSummary): number[] {
  const count = airedEpisodeCount(media)
  if (count <= MAX_EPISODE_BUTTONS) return Array.from({ length: count }, (_, index) => index + 1)
  const start = Math.max(1, Math.min(count - MAX_EPISODE_BUTTONS + 1, media.watched - 10))
  return Array.from({ length: MAX_EPISODE_BUTTONS }, (_, index) => start + index)
}

/** 从用户明确安装的本地插件流式读取候选，由用户选定后才播放。 */
export function MediaSourceButton({ media }: { media: MediaSummary }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const active = useRef<AbortController | null>(null)
  const torrent = useTorrentPlayback()
  const [manualEpisode, setManualEpisode] = useState(String(Math.max(1, media.watched + 1)))
  const [state, setState] = useState<CandidateState>({ phase: 'idle' })
  const [playingID, setPlayingID] = useState<string | null>(null)
  const [playError, setPlayError] = useState<string | null>(null)
  const numbers = episodeNumbers(media)
  const titleByEpisode = new Map(media.episodeTitles?.map(item => [item.episode, item.title]) ?? [])

  useEffect(() => () => active.current?.abort(), [])
  useEffect(() => {
    active.current?.abort()
    setManualEpisode(String(Math.max(1, media.watched + 1)))
    setState({ phase: 'idle' })
    setPlayingID(null)
    setPlayError(null)
  }, [media.id, media.watched])

  async function findEpisode(episode: number): Promise<void> {
    if (!Number.isSafeInteger(episode) || episode < 1) return
    active.current?.abort()
    const controller = new AbortController()
    active.current = controller
    setState({ phase: 'idle' })
    setPlayError(null)
    try {
      const plugin = await fetchSourcePlugin()
      if (controller.signal.aborted) return
      if (!plugin.config.enabled || plugin.status.phase !== 'ready') {
        setState({ phase: 'error', episode, message: '本地来源插件尚未就绪，请先到设置页完成配置' })
        return
      }
      const progress: CandidateProgress = { episode, plugin, candidates: [], errors: [] }
      setState({ phase: 'searching', ...progress })
      await streamSourceCandidates(resolveRequest(media, episode, titleByEpisode.get(episode)), event => {
        if (controller.signal.aborted) return
        setState(current => {
          if (current.phase !== 'searching' || current.episode !== episode) return current
          if (event.event === 'candidate') {
            if (current.candidates.length >= MAX_CANDIDATES) return current
            return { ...current, candidates: [...current.candidates, event.candidate] }
          }
          if (event.event === 'source_error') {
            const source = sourceName(current.plugin.sources, event.sourceId)
            return { ...current, errors: [...current.errors, `${source}：${event.message}`] }
          }
          if (event.event === 'done') return { ...current, phase: 'ready' }
          return current
        })
      }, controller.signal)
    } catch (err) {
      if (!controller.signal.aborted) {
        setState({ phase: 'error', episode, message: errorText(err, '查找在线来源失败') })
      }
    } finally {
      if (active.current === controller) active.current = null
    }
  }

  function submitManual(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    void findEpisode(Number(manualEpisode))
  }

  async function start(candidate: SourceCandidate, episode: number, button: HTMLButtonElement): Promise<void> {
    const title = candidate.match.subjectTitle?.trim() || media.title
    setPlayError(null)
    if (candidate.transport.type === 'torrent') {
      const magnet = candidateMagnet(candidate)
      if (magnet === null) {
        setPlayError('这条 BT 候选只提供了种子文件地址，当前版本尚不能播放')
        return
      }
      torrent.play(
        { magnet, title, episodeHint: episode, fileIndex: candidate.transport.fileIndex },
        title,
        () => button.isConnected ? button.focus() : trigger.current?.focus(),
      )
      dialog.current?.close()
      return
    }

    setPlayingID(candidate.id)
    try {
      await playSourceCandidate(candidate, title, episode)
      dialog.current?.close()
    } catch (err) {
      setPlayError(errorText(err, '在线播放启动失败'))
    } finally {
      setPlayingID(null)
    }
  }

  const selectedEpisode = state.phase === 'idle' ? null : state.episode
  return <>
    <button type="button" className="discover-card-play discover-card-source" ref={trigger}
      aria-label={`选择集数并从本地插件找源：${media.title}`}
      onClick={() => dialog.current?.showModal()}>
      <Icon name="extension" size={18} />在线找源
    </button>
    <dialog ref={dialog} className="media-play-dialog media-source-dialog" aria-label={`${media.title} 在线来源选集`}
      onClose={() => {
        active.current?.abort()
        active.current = null
        setState({ phase: 'idle' })
        setPlayError(null)
        trigger.current?.focus()
      }}
      onClick={event => { if (event.target === dialog.current) dialog.current.close() }}>
      <div className="media-play-body">
        <button type="button" className="icon-button media-play-close" aria-label="关闭在线来源选集" onClick={() => dialog.current?.close()}><Icon name="close" /></button>
        <p className="media-play-kicker">本地来源插件</p>
        <h2>{media.title}</h2>
        <p className="media-play-hint">选择集数后，候选会随插件返回实时出现。不会自动播放；你选定一条后才会交给 mpv 或磁力引擎。</p>

        {numbers.length > 0 && <section className="media-episode-section" aria-labelledby={`source-episodes-${media.id}`}>
          <div className="media-play-selection"><h3 id={`source-episodes-${media.id}`}>{media.format === 'MOVIE' ? '影片' : '选择集数'}</h3><span className="result result--dim">已播出 {airedEpisodeCount(media)} 集</span></div>
          <div className="media-episode-grid">
            {numbers.map(episode => <button type="button" key={episode}
              className={selectedEpisode === episode ? 'media-episode-button media-episode-button--selected' : 'media-episode-button'}
              title={titleByEpisode.get(episode)} aria-label={`从本地插件查找第 ${episode} 集`}
              onClick={() => { setManualEpisode(String(episode)); void findEpisode(episode) }}>
              <strong>{media.format === 'MOVIE' ? '播放' : episode}</strong>
              {titleByEpisode.has(episode) && <span>{titleByEpisode.get(episode)}</span>}
            </button>)}
          </div>
        </section>}

        <form className="media-manual-episode" onSubmit={submitManual}>
          <label><span>{numbers.length ? '指定其他集' : '目标集数'}</span><input className="input" type="number" min="1" step="1" required value={manualEpisode} onChange={event => setManualEpisode(event.target.value)} /></label>
          <button type="submit" className="btn btn--primary" disabled={state.phase === 'searching'}>{state.phase === 'searching' ? '找源中…' : '查找来源'}</button>
          {state.phase === 'searching' && <button type="button" className="btn" onClick={() => { active.current?.abort(); setState({ phase: 'idle' }) }}>取消</button>}
        </form>

        {state.phase === 'error' && <p className="result result--err" role="alert">{state.message} <a className="link" href="/settings#sources">前往设置</a></p>}
        {(state.phase === 'searching' || state.phase === 'ready') && <CandidateResults state={state} busy={torrent.busy || playingID !== null}
          playingID={playingID} playError={playError} onPlay={start} />}
      </div>
    </dialog>
  </>
}

function CandidateResults({ state, busy, playingID, playError, onPlay }: {
  state: Extract<CandidateState, { phase: 'searching' | 'ready' }>
  busy: boolean
  playingID: string | null
  playError: string | null
  onPlay: (candidate: SourceCandidate, episode: number, button: HTMLButtonElement) => Promise<void>
}) {
  return <section className="media-resource-results" aria-label={`第 ${state.episode} 集在线来源`}>
    <div className="media-play-selection"><h3>第 {state.episode} 集候选</h3><span className="result result--dim">{state.candidates.length} 条{state.phase === 'searching' ? ' · 继续接收中' : ''}</span></div>
    {state.errors.length > 0 && <ul className="media-source-errors" aria-label="来源错误">{state.errors.map((error, index) => <li key={`${index}-${error}`}>{error}</li>)}</ul>}
    {playError && <p className="result result--err" role="alert">{playError}</p>}
    {state.candidates.length === 0 ? <p className="media-play-hint" role="status">{state.phase === 'searching' ? '正在等待第一条候选…' : '所有启用的来源都没有返回可播候选。'}</p> :
      <ul className="media-resource-list">{state.candidates.map(candidate => <li key={`${candidate.sourceId}-${candidate.id}`}>
        <div><strong>{candidateTitle(candidate)}</strong><span>{candidateDetails(candidate, state.plugin.sources)}</span></div>
        <button type="button" className="btn btn--sm btn--primary" disabled={busy}
          onClick={event => void onPlay(candidate, state.episode, event.currentTarget)}>{playingID === candidate.id ? '启动中…' : '播放'}</button>
      </li>)}</ul>}
    {state.candidates.length >= MAX_CANDIDATES && <p className="media-play-hint">只显示最先返回的 {MAX_CANDIDATES} 条候选。</p>}
  </section>
}

function resolveRequest(media: MediaSummary, episode: number, episodeTitle?: string): SourceResolveRequest {
  const titles = [...new Set([media.title, media.titleNative, media.titleEnglish].filter((title): title is string => typeof title === 'string' && title.trim() !== '').map(title => title.trim()))]
  return {
    schema: 'nagare-resolve-request/v1',
    subject: { ids: { anilist: String(media.id) }, titles, ...(media.year === undefined ? {} : { year: media.year }) },
    episode: { number: String(episode), absolute: episode, ...(episodeTitle === undefined ? {} : { title: episodeTitle }) },
    preferences: { preferredTransports: ['hls', 'http', 'torrent'] },
  }
}

function candidateTitle(candidate: SourceCandidate): string {
  const transport: Record<SourceCandidate['transport']['type'], string> = { hls: 'HLS', http: 'HTTP', torrent: 'BT' }
  return [candidate.metadata.resolution, candidate.metadata.fansub, transport[candidate.transport.type]].filter(Boolean).join(' · ') || '可播候选'
}

function candidateDetails(candidate: SourceCandidate, sources: PluginSource[]): string {
  const details = [
    sourceName(sources, candidate.sourceId),
    `${Math.round(candidate.matchConfidence * 100)}% 匹配`,
    candidate.metadata.subtitleLanguages?.join('/'),
    candidate.metadata.sizeBytes ? formatBytes(candidate.metadata.sizeBytes) : null,
    typeof candidate.metadata.seeders === 'number' ? `做种 ${candidate.metadata.seeders}` : null,
  ]
  return details.filter(Boolean).join(' · ')
}

function sourceName(sources: PluginSource[], id: string): string {
  return sources.find(source => source.id === id)?.name ?? id
}

function candidateMagnet(candidate: SourceCandidate): string | null {
  if (candidate.transport.magnet) return candidate.transport.magnet
  if (!candidate.transport.infoHash) return null
  const params = new URLSearchParams({ xt: `urn:btih:${candidate.transport.infoHash}` })
  for (const tracker of candidate.transport.trackers ?? []) params.append('tr', tracker)
  return `magnet:?${params.toString()}`
}
