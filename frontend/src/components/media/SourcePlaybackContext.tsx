import { createContext, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { useTorrentPlayback } from '../torrent/TorrentPlayContext'
import {
  fetchPlayerStatus,
  fetchSourcePlugin,
  playSourceCandidate,
  streamSourceCandidates,
} from '../../lib/endpoints'
import type {
  PluginSource,
  SourceCandidate,
  SourcePluginView,
  SourceResolveRequest,
  TorrentPlayRequest,
} from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import type { MediaSummary } from './types'

const PLAYER_MONITOR_INTERVAL_MS = 1000

export interface SourcePlaybackRequest {
  media: Pick<MediaSummary, 'id' | 'title' | 'titleNative' | 'titleEnglish' | 'year'>
  episode: number
  episodeTitle?: string
}

export type SourcePlaybackPhase =
  | 'checking'
  | 'resolving'
  | 'starting'
  | 'playing'
  | 'fallback'
  | 'ready'
  | 'error'

export interface SourcePlaybackSession {
  phase: SourcePlaybackPhase
  request: SourcePlaybackRequest
  plugin?: SourcePluginView
  candidates: SourceCandidate[]
  sourceErrors: string[]
  attemptedIDs: string[]
  streamDone: boolean
  activeCandidateID?: string
  activeFileID?: string
  message?: string
}

export type SourcePlaybackState = { phase: 'idle' } | SourcePlaybackSession

interface SourcePlaybackValue {
  state: SourcePlaybackState
  busy: boolean
  resolve: (request: SourcePlaybackRequest) => void
  play: (candidate: SourceCandidate) => void
  cancelSearch: () => void
  dismissError: () => void
}

interface RuntimeSession {
  generation: number
  request: SourcePlaybackRequest
  controller: AbortController
  plugin?: SourcePluginView
  candidates: SourceCandidate[]
  sourceErrors: string[]
  attempted: Set<string>
  streamDone: boolean
  pending: boolean
  phase: SourcePlaybackPhase
  activeCandidateID?: string
  activeFileID?: string
  message?: string
}

const fallback: SourcePlaybackValue = {
  state: { phase: 'idle' }, busy: false, resolve: () => {}, play: () => {},
  cancelSearch: () => {}, dismissError: () => {},
}
const SourcePlaybackContext = createContext<SourcePlaybackValue | null>(null)

/**
 * 来源播放会话挂在根布局：关闭选集窗或切换页面不会丢掉
 * 尚未返回的候选与自动回退状态。
 */
export function SourcePlaybackProvider({ children }: { children: ReactNode }) {
  const torrent = useTorrentPlayback()
  const torrentRef = useRef(torrent)
  torrentRef.current = torrent
  const current = useRef<RuntimeSession | null>(null)
  const generation = useRef(0)
  const chooseNextRef = useRef<(session: RuntimeSession) => void>(() => {})
  const [state, setState] = useState<SourcePlaybackState>({ phase: 'idle' })

  function publish(session: RuntimeSession, phase?: SourcePlaybackPhase, message?: string): void {
    if (current.current !== session) return
    if (phase !== undefined) session.phase = phase
    if (message !== undefined) session.message = message
    setState({
      phase: session.phase,
      request: session.request,
      ...(session.plugin === undefined ? {} : { plugin: session.plugin }),
      candidates: [...session.candidates],
      sourceErrors: [...session.sourceErrors],
      attemptedIDs: [...session.attempted],
      streamDone: session.streamDone,
      ...(session.activeCandidateID === undefined ? {} : { activeCandidateID: session.activeCandidateID }),
      ...(session.activeFileID === undefined ? {} : { activeFileID: session.activeFileID }),
      ...(session.message === undefined ? {} : { message: session.message }),
    })
  }

  async function attemptOnline(session: RuntimeSession, candidate: SourceCandidate): Promise<void> {
    if (current.current !== session) return
    session.pending = true
    session.attempted.add(candidate.id)
    session.activeCandidateID = candidate.id
    session.activeFileID = undefined
    publish(session, 'starting', `正在尝试 ${candidateLabel(candidate, session.plugin?.sources ?? [])}`)
    try {
      const result = await playSourceCandidate(candidate, playbackTitle(candidate, session.request), session.request.episode)
      if (current.current !== session || session.activeCandidateID !== candidate.id) return
      if (!result.fileId) throw new Error('后端未返回播放会话标识')
      session.pending = false
      session.activeFileID = result.fileId
      publish(session, 'playing', `正在播放 ${candidateLabel(candidate, session.plugin?.sources ?? [])}`)
    } catch (err) {
      if (current.current !== session || session.activeCandidateID !== candidate.id) return
      session.pending = false
      session.activeCandidateID = undefined
      session.activeFileID = undefined
      session.sourceErrors.push(`${candidateLabel(candidate, session.plugin?.sources ?? [])}：${errorText(err, '播放启动失败')}`)
      publish(session, 'fallback', '当前在线候选失败，正在尝试下一条')
      chooseNextRef.current(session)
    }
  }

  function startTorrent(session: RuntimeSession, candidate: SourceCandidate): void {
    const locator = candidateTorrentLocator(candidate)
    session.attempted.add(candidate.id)
    if (locator === null) {
      session.sourceErrors.push(`${candidateLabel(candidate, session.plugin?.sources ?? [])}：缺少可用的 BT 入口`)
      session.activeCandidateID = undefined
      publish(session, 'fallback', '无法使用这条 BT 候选，正在尝试下一条')
      chooseNextRef.current(session)
      return
    }
    session.pending = false
    session.activeFileID = undefined
    session.activeCandidateID = candidate.id
    const title = playbackTitle(candidate, session.request)
    const torrentRequest: TorrentPlayRequest = 'magnet' in locator
      ? { magnet: locator.magnet, title, episodeHint: session.request.episode, fileIndex: candidate.transport.fileIndex }
      : { torrentUrl: locator.torrentUrl, title, episodeHint: session.request.episode, fileIndex: candidate.transport.fileIndex }
    torrentRef.current.play(
      torrentRequest,
      title,
    )
    publish(session, 'playing', `已回退到 ${candidateLabel(candidate, session.plugin?.sources ?? [])}`)
  }

  function chooseNext(session: RuntimeSession): void {
    if (current.current !== session || session.pending || session.activeFileID !== undefined) return
    const candidates = sortCandidates(session.candidates.filter(candidate => !session.attempted.has(candidate.id)))
    const online = candidates.filter(candidate => candidate.transport.type !== 'torrent')
    const nextOnline = session.streamDone
      ? online[0]
      : online.find(candidate => candidate.tier <= 1 && candidate.matchConfidence >= 0.8)
    if (nextOnline !== undefined) {
      void attemptOnline(session, nextOnline)
      return
    }
    if (!session.streamDone) {
      publish(session, 'resolving', '正在等待高优先级在线候选')
      return
    }
    const nextTorrent = candidates.find(candidate => candidate.transport.type === 'torrent')
    if (nextTorrent !== undefined) {
      startTorrent(session, nextTorrent)
      return
    }
    const message = session.candidates.length === 0
      ? '所有来源都没有返回可播候选'
      : '所有候选都已尝试，可在列表中手动重试'
    publish(session, session.sourceErrors.length > 0 ? 'error' : 'ready', message)
  }
  chooseNextRef.current = chooseNext

  function resolve(request: SourcePlaybackRequest): void {
    current.current?.controller.abort()
    const controller = new AbortController()
    const session: RuntimeSession = {
      generation: ++generation.current, request, controller, candidates: [], sourceErrors: [],
      attempted: new Set(), streamDone: false, pending: false, phase: 'checking',
      message: '正在检查本地来源插件',
    }
    current.current = session
    publish(session)
    void (async () => {
      try {
        const plugin = await fetchSourcePlugin(controller.signal)
        if (current.current !== session || controller.signal.aborted) return
        session.plugin = plugin
        if (!plugin.config.enabled || plugin.status.phase !== 'ready') {
          session.streamDone = true
          publish(session, 'error', '本地来源插件尚未就绪，请先到设置页完成配置')
          return
        }
        publish(session, 'resolving', '正在从已启用来源查找候选')
        await streamSourceCandidates(resolveRequest(request), event => {
          if (current.current !== session || controller.signal.aborted) return
          if (event.event === 'candidate') {
            if (!session.candidates.some(candidate => candidate.id === event.candidate.id)) {
              session.candidates.push(event.candidate)
            }
            publish(session)
            chooseNext(session)
          } else if (event.event === 'source_error') {
            session.sourceErrors.push(`${sourceName(plugin.sources, event.sourceId)}：${sourceErrorText(event.category, event.message)}`)
            publish(session)
          } else if (event.event === 'done') {
            session.streamDone = true
            publish(session)
            chooseNext(session)
          }
        }, controller.signal)
      } catch (err) {
        if (current.current !== session || controller.signal.aborted) return
        session.streamDone = true
        session.sourceErrors.push(errorText(err, '候选流中断'))
        publish(session, 'fallback', '候选流中断，正在尝试已收到的候选')
        chooseNext(session)
      }
    })()
  }

  function play(candidate: SourceCandidate): void {
    const session = current.current
    if (session === null || session.pending) return
    session.attempted.delete(candidate.id)
    session.activeFileID = undefined
    session.activeCandidateID = candidate.id
    if (candidate.transport.type === 'torrent') startTorrent(session, candidate)
    else void attemptOnline(session, candidate)
  }

  function cancelSearch(): void {
    const session = current.current
    if (session === null) return
    session.controller.abort()
    session.streamDone = true
    if (session.activeFileID === undefined && !session.pending) {
      publish(session, 'ready', '已停止继续找源，已收到的候选仍可手动播放')
    } else {
      publish(session)
    }
  }

  function dismissError(): void {
    const session = current.current
    if (session === null || session.phase !== 'error') return
    publish(session, 'ready')
  }

  useEffect(() => () => current.current?.controller.abort(), [])

  const activeFileID = state.phase === 'idle' ? undefined : state.activeFileID
  useEffect(() => {
    if (activeFileID === undefined) return
    let disposed = false
    let inFlight = false
    const tick = async () => {
      if (disposed || inFlight) return
      inFlight = true
      try {
        const player = await fetchPlayerStatus()
        if (disposed) return
        const session = current.current
        if (session === null || session.activeFileID !== activeFileID) return
        if (player.playbackFailure?.fileId === activeFileID) {
          session.sourceErrors.push(`${candidateLabelByID(session, session.activeCandidateID)}：${player.playbackFailure.reason}`)
          session.activeFileID = undefined
          session.activeCandidateID = undefined
          publish(session, 'fallback', '在线播放失败，正在自动尝试下一条')
          chooseNextRef.current(session)
        } else if (player.playing && player.fileId !== activeFileID) {
          session.activeFileID = undefined
          session.activeCandidateID = undefined
          publish(session, 'ready', '已切换到其他播放会话，自动回退已停止')
        } else if (!player.playing) {
          session.activeFileID = undefined
          session.activeCandidateID = undefined
          publish(session, 'ready', '播放已正常结束，仍可手动选择其他候选')
        }
      } catch {
        // 短暂读不到状态不应终止已启动的播放或自动回退会话。
      } finally {
        inFlight = false
      }
    }
    void tick()
    const timer = setInterval(() => { void tick() }, PLAYER_MONITOR_INTERVAL_MS)
    return () => { disposed = true; clearInterval(timer) }
  }, [activeFileID])

  const busy = state.phase !== 'idle' && (state.phase === 'checking' || state.phase === 'starting')
  return <SourcePlaybackContext.Provider value={{ state, busy, resolve, play, cancelSearch, dismissError }}>
    {children}
    {state.phase === 'error' && <div className="torrent-global-error" role="alert">
      <span>{state.message}</span>
      <a className="btn btn--sm" href="/settings#sources">打开来源设置</a>
      <button type="button" className="btn btn--sm" onClick={dismissError}>关闭</button>
    </div>}
  </SourcePlaybackContext.Provider>
}

export function useSourcePlayback(): SourcePlaybackValue {
  return useContext(SourcePlaybackContext) ?? fallback
}

function resolveRequest(request: SourcePlaybackRequest): SourceResolveRequest {
  const media = request.media
  const titles = [...new Set([media.title, media.titleNative, media.titleEnglish]
    .filter((title): title is string => typeof title === 'string' && title.trim() !== '')
    .map(title => title.trim()))]
  return {
    schema: 'nagare-resolve-request/v1',
    subject: { ids: { anilist: String(media.id) }, titles, ...(media.year === undefined ? {} : { year: media.year }) },
    episode: { number: String(request.episode), absolute: request.episode, ...(request.episodeTitle === undefined ? {} : { title: request.episodeTitle }) },
    preferences: { preferredTransports: ['hls', 'http', 'torrent'] },
  }
}

function sortCandidates(candidates: SourceCandidate[]): SourceCandidate[] {
  const resolution = (value?: string) => Number(value?.match(/\d+/)?.[0] ?? 0)
  return [...candidates].sort((a, b) =>
    a.tier - b.tier ||
    (a.metadata.channelTier ?? 99) - (b.metadata.channelTier ?? 99) ||
    b.matchConfidence - a.matchConfidence ||
    Number(a.transport.type === 'torrent') - Number(b.transport.type === 'torrent') ||
    resolution(b.metadata.resolution) - resolution(a.metadata.resolution) ||
    (b.metadata.seeders ?? -1) - (a.metadata.seeders ?? -1),
  )
}

function playbackTitle(candidate: SourceCandidate, request: SourcePlaybackRequest): string {
  return candidate.match.subjectTitle?.trim() || request.media.title
}

function candidateLabel(candidate: SourceCandidate, sources: PluginSource[]): string {
  const kind = candidate.transport.type === 'torrent' ? 'BT' : candidate.transport.type.toUpperCase()
  return [sourceName(sources, candidate.sourceId), candidate.metadata.resolution, kind].filter(Boolean).join(' · ')
}

function candidateLabelByID(session: RuntimeSession, id?: string): string {
  const candidate = session.candidates.find(item => item.id === id)
  return candidate === undefined ? '在线候选' : candidateLabel(candidate, session.plugin?.sources ?? [])
}

function sourceName(sources: PluginSource[], id: string): string {
  return sources.find(source => source.id === id)?.name ?? id
}

function sourceErrorText(category: string, fallback: string): string {
  const messages: Record<string, string> = {
    source_disabled: '来源已停用',
    interactive_required: '来源需要人工验证',
    search_timeout: '搜索来源超时',
    search_failed: '搜索来源失败',
    no_subject_match: '没有找到可靠的作品匹配',
    episode_not_found: '来源中没有这一集',
    episode_ambiguous: '来源中的集号无法确定',
    resolve_timeout: '解析播放地址超时',
    resolve_failed: '解析播放地址失败',
    browser_blocked: '浏览器解析被站点阻止',
    unsafe_redirect: '跳转越过了来源的安全边界',
    invalid_candidate: '来源返回的候选格式无效',
    cancelled: '查找已取消',
  }
  return messages[category] ?? fallback
}

function candidateTorrentLocator(candidate: SourceCandidate): { magnet: string } | { torrentUrl: string } | null {
  if (candidate.transport.magnet) return { magnet: candidate.transport.magnet }
  if (!candidate.transport.infoHash) {
    return candidate.transport.torrentUrl ? { torrentUrl: candidate.transport.torrentUrl } : null
  }
  const params = new URLSearchParams({ xt: `urn:btih:${candidate.transport.infoHash}` })
  for (const tracker of candidate.transport.trackers ?? []) params.append('tr', tracker)
  return { magnet: `magnet:?${params.toString()}` }
}
