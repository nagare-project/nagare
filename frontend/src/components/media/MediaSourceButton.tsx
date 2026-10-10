import { episodeHasAired } from './episode-metadata'
import { EpisodeArtwork } from './EpisodeArtwork'
import { PlaybackSurface } from './PlaybackSurface'
import { useRef, useState } from 'react'
import type { FormEvent } from 'react'
import type { PluginSource, SourceCandidate } from '../../lib/endpoints'
import { formatBytes } from '../../lib/format'
import { Icon } from '../ui/Icon'
import { airedEpisodeCount } from './releaseEpisodes'
import { sortCandidates, useSourcePlayback } from './SourcePlaybackContext'
import { candidateChineseScore } from './chineseSubtitles'
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

/**
 * 选集即开始找源；高优先级在线候选先起播。mpv 一打开就停止找源、不再自动起播
 * （见 SourcePlaybackContext 的 auto），其余候选留在列表里手动换源。
 */
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
        {!inline && <p className="media-play-hint">点选集数后立即开始找源。高优先级在线候选会先起播，启动不了时自动尝试下一在线来源或 BT；mpv 打开后就停止找源，不会再自动打开新的窗口，列表始终可手动换源。</p>}
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

/** 收起时每个标签页列几条：结果区不能把下面的选集挤到屏幕外 */
const COLLAPSED_CANDIDATES = 4

type CandidateKind = 'online' | 'bt'

const kindOf = (candidate: SourceCandidate): CandidateKind => candidate.transport.type === 'torrent' ? 'bt' : 'online'

const STATUS_LABELS: Record<SourcePlaybackSession['phase'], string> = {
  checking: '检查插件', resolving: '找源中', starting: '启动中', playing: '播放中', fallback: '换源中', ready: '待选择', error: '出错',
}

function statusTone(session: SourcePlaybackSession): 'busy' | 'ok' | 'err' | 'idle' {
  if (session.phase === 'playing') return 'ok'
  if (session.phase === 'error') return 'err'
  if (session.phase === 'ready') return 'idle'
  return 'busy'
}

/**
 * 一集的找源结果：顶部一张状态卡（集数、状态、一句说明，报错的来源折叠起来），
 * 下面按「在线 / BT」分开列候选，中文字幕优先，收起时每类只列前几条。
 */
function CandidateResults({ session, busy, onPlay }: {
  session: SourcePlaybackSession
  busy: boolean
  onPlay: (candidate: SourceCandidate) => void
}) {
  const [picked, setPicked] = useState<CandidateKind | null>(null)
  const [expanded, setExpanded] = useState(false)
  const sources = session.plugin?.sources ?? []
  const sorted = sortCandidates(session.candidates).slice(0, MAX_CANDIDATES)
  const lists: Record<CandidateKind, SourceCandidate[]> = {
    online: sorted.filter(candidate => kindOf(candidate) === 'online'),
    bt: sorted.filter(candidate => kindOf(candidate) === 'bt'),
  }
  const activeCandidate = session.candidates.find(candidate => candidate.id === session.activeCandidateID)
  // 用户没点过标签时跟着正在播的那条走（自动回退到 BT 就切到 BT），否则有在线候选先看在线
  const kind: CandidateKind = picked ?? (activeCandidate ? kindOf(activeCandidate) : lists.online.length > 0 ? 'online' : 'bt')
  const list = lists[kind]
  const visible = expanded ? list : list.slice(0, COLLAPSED_CANDIDATES)
  // 正在播的那条始终看得见
  if (activeCandidate && kindOf(activeCandidate) === kind && !visible.includes(activeCandidate)) visible.push(activeCandidate)
  const hidden = list.length - visible.length
  const tone = statusTone(session)
  const episodeTitle = session.request.episodeTitle
  const choose = (next: CandidateKind) => { setPicked(next); setExpanded(false) }

  return <section className="media-resource-results source-results" aria-label={`第 ${session.request.episode} 集在线来源`}>
    <div className="source-results-head">
      <h3>第 {session.request.episode} 集{episodeTitle ? <span> · {episodeTitle}</span> : null}</h3>
      <span className={`source-status source-status--${tone}`}>{STATUS_LABELS[session.phase]}</span>
      <span className="result result--dim">{session.candidates.length} 条候选{session.streamDone ? '' : ' · 继续接收中'}</span>
    </div>
    {session.message && <p className={tone === 'err' ? 'result result--err' : 'source-results-message'} role="status">{session.message}{tone === 'err' && <> <a className="link" href="/settings#sources">检查设置</a></>}</p>}
    {session.sourceErrors.length > 0 && <details className="source-issues">
      <summary>{session.sourceErrors.length} 个来源没能用上</summary>
      <ul className="media-source-errors" aria-label="来源错误">{session.sourceErrors.map((error, index) => <li key={`${index}-${error}`}>{error}</li>)}</ul>
    </details>}
    {session.candidates.length === 0 ? <p className="media-play-hint" role="status">{session.streamDone ? '没有可播候选。' : '正在等待第一条候选…'}</p> : <>
      <div className="source-kinds">
        <div className="source-kind-row" role="group" aria-label="候选类型">
          {(['online', 'bt'] as const).map(item => <button type="button" key={item} aria-pressed={kind === item}
            className={kind === item ? 'source-kind source-kind--active' : 'source-kind'} onClick={() => choose(item)}>
            {item === 'online' ? '在线' : 'BT'}<span>{lists[item].length}</span>
          </button>)}
        </div>
        <span className="source-kinds-note">中文字幕优先</span>
      </div>
      {list.length === 0 ? <p className="media-play-hint" role="status">{kind === 'online' ? '没有在线候选。' : '没有 BT 候选。'}</p> :
        <ul className="media-resource-list source-candidate-list">{visible.map(candidate => {
          const active = session.activeCandidateID === candidate.id
          const attempted = session.attemptedIDs.includes(candidate.id)
          return <li key={`${candidate.sourceId}-${candidate.id}`} className={active ? 'media-resource-active' : undefined}>
            <div>
              <strong title={candidate.metadata.title}>
                {candidate.metadata.resolution && <span className="badge">{candidate.metadata.resolution}</span>}
                {candidateChineseScore(candidate) === 2 && <span className="badge badge--accent">中字</span>}
                <span className="source-candidate-name">{candidateName(candidate, sources)}</span>
              </strong>
              <span>{candidateDetails(candidate, sources)}</span>
            </div>
            <button type="button" className="btn btn--sm btn--primary" disabled={busy && !active}
              onClick={() => onPlay(candidate)}>{active ? (session.phase === 'starting' ? '启动中…' : '正在播放') : attempted ? '重试' : '播放'}</button>
          </li>
        })}</ul>}
      {(hidden > 0 || expanded) && list.length > COLLAPSED_CANDIDATES && <button type="button" className="link source-expand" onClick={() => setExpanded(value => !value)}>
        {expanded ? '收起' : `显示全部 ${list.length} 条`}
      </button>}
    </>}
    {session.candidates.length > MAX_CANDIDATES && <p className="media-play-hint">只列出排在最前的 {MAX_CANDIDATES} 条候选。</p>}
  </section>
}

function sourceName(sources: PluginSource[], id: string): string {
  return sources.find(source => source.id === id)?.name ?? id
}

/** 一行的主名字：在线候选是「来源 · 线路」，BT 候选是字幕组（不知道时用来源名） */
function candidateName(candidate: SourceCandidate, sources: PluginSource[]): string {
  if (candidate.transport.type === 'torrent') return candidate.metadata.fansub || sourceName(sources, candidate.sourceId)
  return [sourceName(sources, candidate.sourceId), candidate.metadata.channel].filter(Boolean).join(' · ')
}

function candidateDetails(candidate: SourceCandidate, sources: PluginSource[]): string {
  const torrent = candidate.transport.type === 'torrent'
  const details = [
    torrent && candidate.metadata.fansub ? sourceName(sources, candidate.sourceId) : null,
    candidate.metadata.subtitleLanguages?.join('/'),
    candidate.metadata.sizeBytes ? formatBytes(candidate.metadata.sizeBytes) : null,
    typeof candidate.metadata.seeders === 'number' ? `做种 ${candidate.metadata.seeders}` : null,
    `${Math.round(candidate.matchConfidence * 100)}% 匹配`,
  ]
  return details.filter(Boolean).join(' · ')
}
