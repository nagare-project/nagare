import { episodeHasAired } from './episode-metadata'
import { EpisodeArtwork } from './EpisodeArtwork'
import { PlaybackSurface } from './PlaybackSurface'
import { useRef, useState } from 'react'
import type { FormEvent } from 'react'
import type { PluginSource, SourceCandidate } from '../../lib/endpoints'
import { formatBytes } from '../../lib/format'
import { Icon } from '../ui/Icon'
import { airedEpisodeCount } from './MediaTorrentButton'
import { useSourcePlayback } from './SourcePlaybackContext'
import type { SourcePlaybackSession } from './SourcePlaybackContext'
import type { MediaSummary } from './types'
import './media-play.css'

const MAX_EPISODE_BUTTONS = 120
const MAX_CANDIDATES = 100

function episodeNumbers(media: MediaSummary): number[] {
  const known = media.episodeTitles?.filter(ep => episodeHasAired(ep, media)).map(ep => ep.episode).sort((a, b) => a - b)
  const hasDates = media.episodeTitles?.some(ep => ep.airedAt || ep.airDate)
  if (hasDates) return (known ?? []).slice(0, MAX_EPISODE_BUTTONS)
  const count = airedEpisodeCount(media)
  if (count <= MAX_EPISODE_BUTTONS) return Array.from({ length: count }, (_, index) => index + 1)
  const start = Math.max(1, Math.min(count - MAX_EPISODE_BUTTONS + 1, media.watched - 10))
  return Array.from({ length: MAX_EPISODE_BUTTONS }, (_, index) => start + index)
}

/** 选集即开始找源；高优先级在线候选先起播，其余候选保留供回退与手动换源。 */
export function MediaSourceButton({ media, inline = false }: { media: MediaSummary; inline?: boolean }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const source = useSourcePlayback()
  const [manualEpisode, setManualEpisode] = useState(String(Math.max(1, media.watched + 1)))
  const [gridView, setGridView] = useState(false)
  const numbers = episodeNumbers(media)
  const titleByEpisode = new Map(media.episodeTitles?.map(item => [item.episode, item.title]) ?? [])
  const session = source.state.phase !== 'idle' && source.state.request.media.id === media.id
    ? source.state
    : null

  function findEpisode(episode: number): void {
    if (!Number.isSafeInteger(episode) || episode < 1) return
    const episodeTitle = titleByEpisode.get(episode)
    source.resolve({
      media: {
        id: media.id, title: media.title,
        ...(media.titleNative === undefined ? {} : { titleNative: media.titleNative }),
        ...(media.titleEnglish === undefined ? {} : { titleEnglish: media.titleEnglish }),
        ...(media.year === undefined ? {} : { year: media.year }),
      },
      episode,
      ...(episodeTitle === undefined ? {} : { episodeTitle }),
    })
  }

  function submitManual(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    findEpisode(Number(manualEpisode))
  }

  const selectedEpisode = session?.request.episode ?? null
  const searching = session !== null && !session.streamDone
  return <>
    {!inline && <button type="button" className="discover-card-play discover-card-source" ref={trigger}
      aria-label={`选择集数并从本地插件找源：${media.title}`}
      onClick={() => dialog.current?.showModal()}>
      <Icon name="extension" size={18} />在线找源
    </button>}
    <PlaybackSurface inline={inline} ref={dialog} className="media-play-dialog media-source-dialog" aria-label={`${media.title} 在线来源选集`}
      onClose={() => trigger.current?.focus()}
      onClick={event => { if (event.target === dialog.current) dialog.current.close() }}>
      <div className="media-play-body">
        {!inline && <button type="button" className="icon-button media-play-close" aria-label="关闭在线来源选集" onClick={() => dialog.current?.close()}><Icon name="close" /></button>}
        <p className="media-play-kicker">本地来源插件</p>
        <h2 className={inline ? 'visually-hidden' : undefined}>{inline ? '在线选集' : media.title}</h2>
        {!inline && <p className="media-play-hint">点选集数后立即开始找源。高优先级在线候选会先起播；失败时自动尝试下一在线来源或 BT，列表始终可手动换源。</p>}
        {inline && <div className="online-episode-toolbar"><span><Icon name="broadcast" size={18} />自动匹配来源</span><a className="link" href="/settings#sources"><Icon name="settings" size={16} />来源设置</a><button type="button" className="icon-button" aria-label={gridView ? '切换列表视图' : '切换网格视图'} onClick={() => setGridView(value => !value)}><Icon name={gridView ? 'lists' : 'grid'} size={18} /></button></div>}

        <form className="media-manual-episode" onSubmit={submitManual}>
          <label><span>{numbers.length ? '指定其他集' : '目标集数'}</span><input className="input" type="number" min="1" step="1" required value={manualEpisode} onChange={event => setManualEpisode(event.target.value)} /></label>
          <button type="submit" className="btn btn--primary" disabled={source.busy}>{source.busy ? '处理中…' : '查找并播放'}</button>
          {searching && <button type="button" className="btn" onClick={source.cancelSearch}>停止继续找源</button>}
        </form>

        {session !== null && <CandidateResults session={session} busy={source.busy} onPlay={source.play} />}

        {numbers.length > 0 && <section className="media-episode-section" aria-labelledby={`source-episodes-${media.id}`}>
          <div className="media-play-selection"><h3 id={`source-episodes-${media.id}`}>{media.format === 'MOVIE' ? '影片' : '选择集数'}</h3><span className="result result--dim">已播出 {numbers.length} 集</span></div>
          <div className={inline && !gridView ? "media-episode-grid media-episode-grid--list" : "media-episode-grid"}>
            {numbers.map(episode => <button type="button" key={episode}
              className={selectedEpisode === episode ? 'media-episode-button media-episode-button--selected' : 'media-episode-button'}
              title={titleByEpisode.get(episode)} aria-label={`从本地插件查找第 ${episode} 集`}
              onClick={() => { setManualEpisode(String(episode)); findEpisode(episode) }}>
              {inline && <span className="media-source-still"><EpisodeArtwork image={media.episodeTitles?.find(ep => ep.episode === episode)?.image} banner={media.banner} cover={media.cover} /></span>}
              {inline ? <span className="online-episode-copy"><span>第 {episode} 集 <small>{media.episodeTitles?.find(ep => ep.episode === episode)?.duration || media.duration || ''}{(media.episodeTitles?.find(ep => ep.episode === episode)?.duration || media.duration) ? ' 分钟' : ''}</small></span><strong>{titleByEpisode.get(episode) || `第 ${episode} 集`}</strong>{media.episodeTitles?.find(ep => ep.episode === episode)?.description && <span className="online-episode-description">{media.episodeTitles.find(ep => ep.episode === episode)!.description}</span>}</span> : <><strong>{media.format === 'MOVIE' ? '播放' : episode}</strong>{titleByEpisode.has(episode) && <span>{titleByEpisode.get(episode)}</span>}</>}
            </button>)}
          </div>
        </section>}


      </div>
    </PlaybackSurface>
  </>
}

function CandidateResults({ session, busy, onPlay }: {
  session: SourcePlaybackSession
  busy: boolean
  onPlay: (candidate: SourceCandidate) => void
}) {
  const hasError = session.phase === 'error'
  return <section className="media-resource-results" aria-label={`第 ${session.request.episode} 集在线来源`}>
    <div className="media-play-selection"><h3>第 {session.request.episode} 集候选</h3><span className="result result--dim">{session.candidates.length} 条{session.streamDone ? '' : ' · 继续接收中'}</span></div>
    {session.message && <p className={hasError ? 'result result--err' : 'result result--dim'} role="status">{session.message}{hasError && <> <a className="link" href="/settings#sources">检查设置</a></>}</p>}
    {session.sourceErrors.length > 0 && <ul className="media-source-errors" aria-label="来源错误">{session.sourceErrors.map((error, index) => <li key={`${index}-${error}`}>{error}</li>)}</ul>}
    {session.candidates.length === 0 ? <p className="media-play-hint" role="status">{session.streamDone ? '没有可播候选。' : '正在等待第一条候选…'}</p> :
      <ul className="media-resource-list">{session.candidates.slice(0, MAX_CANDIDATES).map(candidate => {
        const active = session.activeCandidateID === candidate.id
        const attempted = session.attemptedIDs.includes(candidate.id)
        return <li key={`${candidate.sourceId}-${candidate.id}`} className={active ? 'media-resource-active' : undefined}>
          <div><strong>{candidateTitle(candidate)}</strong><span>{candidateDetails(candidate, session.plugin?.sources ?? [])}</span></div>
          <button type="button" className="btn btn--sm btn--primary" disabled={busy && !active}
            onClick={() => onPlay(candidate)}>{active ? (session.phase === 'starting' ? '启动中…' : '正在播放') : attempted ? '重试' : '播放'}</button>
        </li>
      })}</ul>}
    {session.candidates.length > MAX_CANDIDATES && <p className="media-play-hint">只显示最先返回的 {MAX_CANDIDATES} 条候选。</p>}
  </section>
}

function candidateTitle(candidate: SourceCandidate): string {
  const transport: Record<SourceCandidate['transport']['type'], string> = { hls: 'HLS', http: 'HTTP', torrent: 'BT' }
  return [candidate.metadata.resolution, candidate.metadata.fansub, transport[candidate.transport.type]].filter(Boolean).join(' · ') || '可播候选'
}

function candidateDetails(candidate: SourceCandidate, sources: PluginSource[]): string {
  const details = [
    sources.find(source => source.id === candidate.sourceId)?.name ?? candidate.sourceId,
    `${Math.round(candidate.matchConfidence * 100)}% 匹配`,
    candidate.metadata.subtitleLanguages?.join('/'),
    candidate.metadata.sizeBytes ? formatBytes(candidate.metadata.sizeBytes) : null,
    typeof candidate.metadata.seeders === 'number' ? `做种 ${candidate.metadata.seeders}` : null,
  ]
  return details.filter(Boolean).join(' · ')
}
