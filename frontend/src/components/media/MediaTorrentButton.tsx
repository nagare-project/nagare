import { PlaybackSurface } from './PlaybackSurface'
import { useEffect, useId, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useTorrentPlayback } from '../torrent/TorrentPlayContext'
import { fetchSettings, fetchSources, searchMagnets, streamPluginMagnets } from '../../lib/endpoints'
import type { MagnetSearchContext, SearchItem, SearchResult, SourceOutcome, SourcesData, TorrentPlayRequest } from '../../lib/endpoints'
import { fetchEpisodeOffset, usableOffset } from '../../lib/episodeOffset'
import { errorText } from '../../lib/format'
import { Icon } from '../ui/Icon'
import { airedEpisodeCount, catalogIdentity, defaultTorrentEpisode, episodeChoices, extraKindsFor, MAX_EPISODE, notBeforeFor, playbackHints, seasonOfTitle } from './releaseEpisodes'
import type { EpisodeTarget } from './releaseEpisodes'
import { TorrentReleaseResults } from './TorrentReleaseResults'
import type { MediaSummary } from './types'
import './media-play.css'
import './torrent-episodes.css'

/** 本机规则的结果：与集号无关（规则按作品名搜），换集不重搜 */
type LocalState =
  | { phase: 'idle' }
  | { phase: 'searching' }
  | { phase: 'ready'; query: string; result: SearchResult; sources: SourcesData; engineDown: boolean }
  | { phase: 'error'; message: string }

/**
 * 插件 BT 来源的结果。scope 由插件流的首行告知：all 是按作品搜回的全部发布（与 animego 一样，
 * 换集只重新排序）；episode 是老插件按集号精确筛的结果，换集就要重问；null 是还没收到首行。
 */
interface PluginState {
  /** 这次请求是按哪一集发的：只用来丢弃过期的流（scope=all 时与界面上选的集无关） */
  episode: number
  scope: 'all' | 'episode' | null
  items: SearchItem[]
  outcomes: SourceOutcome[]
  pending: boolean
  error: string | null
}

/**
 * 跨季连续编号的偏移：idle 还没问过；known 才能用；unknown（animego 算不出前作有几集）与
 * failed（这次没查到）都要让用户知道连续编号的发布可能没归到这一集 —— 绝不能当 0 用。
 */
type OffsetState = { status: 'idle' } | { status: 'unknown' } | { status: 'known'; offset: number } | { status: 'failed' }

/**
 * 目录作品的磁力入口：先选集数，再按字幕组浏览发布（这一集的单集与含它的合集排在最前），最后选版本。
 * 作品 ID 不参与找源：磁力始终来自用户自己装的插件与本机规则；ID 只随播放请求留在本机，
 * 用来校验弹幕匹配没有认成别的作品，从不拿它去 animego 换磁力。
 */
export function MediaTorrentButton({ media, onOpenChange, inline = false }: { media: MediaSummary; onOpenChange?: (open: boolean) => void; inline?: boolean }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const requestVersion = useRef(0)
  const pluginAbort = useRef<AbortController | null>(null)
  // 每次换集 / 重试 / 新搜索都 +1：查偏移可能要等几秒，等回来时只有最后一次请求还作数
  const pluginRequest = useRef(0)
  const offsetRequest = useRef<{ mediaId: number; promise: Promise<number | undefined> } | null>(null)
  const torrent = useTorrentPlayback()
  const headingId = useId()
  const isMovie = media.format === 'MOVIE'
  const [query, setQuery] = useState(media.title)
  // 没点过就跟着观看进度走（进度是异步载入的）；点过就以用户选的为准
  const [picked, setPicked] = useState<number | null>(null)
  const [episodeInput, setEpisodeInput] = useState('')
  const [local, setLocal] = useState<LocalState>({ phase: 'idle' })
  const [plugin, setPlugin] = useState<PluginState | null>(null)
  const [offset, setOffset] = useState<OffsetState>({ status: 'idle' })
  const episode = picked ?? defaultTorrentEpisode(media)

  useEffect(() => () => { requestVersion.current += 1; pluginAbort.current?.abort() }, [])
  useEffect(() => {
    setQuery(media.title)
    setPicked(null)
    setEpisodeInput('')
    requestVersion.current++
    pluginAbort.current?.abort()
    offsetRequest.current = null
    setLocal({ phase: 'idle' })
    setPlugin(null)
    setOffset({ status: 'idle' })
  }, [media.id, media.title])

  /** 集号偏移每部作品只问一次；失败了下次操作再问 */
  function loadOffset(): Promise<number | undefined> {
    if (isMovie) return Promise.resolve(undefined)
    const cached = offsetRequest.current
    if (cached !== null && cached.mediaId === media.id) return cached.promise
    const mediaId = media.id
    const promise = fetchEpisodeOffset(mediaId).then(
      value => {
        if (offsetRequest.current?.promise === promise) setOffset(value.known ? { status: 'known', offset: value.offset } : { status: 'unknown' })
        return usableOffset(value)
      },
      (err: unknown) => {
        console.error('查询跨季编号失败', err)
        if (offsetRequest.current?.promise === promise) {
          offsetRequest.current = null
          setOffset({ status: 'failed' })
        }
        return undefined
      },
    )
    offsetRequest.current = { mediaId, promise }
    return promise
  }

  async function searchPlugin(targetEpisode: number, searchQuery: string, offsetValue: number | undefined): Promise<void> {
    pluginAbort.current?.abort()
    const controller = new AbortController()
    pluginAbort.current = controller
    const live = () => pluginAbort.current === controller && !controller.signal.aborted
    const update = (change: (state: PluginState) => PluginState) => setPlugin(state => (state === null || state.episode !== targetEpisode ? state : change(state)))
    setPlugin({ episode: targetEpisode, scope: null, items: [], outcomes: [], pending: true, error: null })
    // 插件按作品身份找：目录里的其他写法照带（与本机规则同一份去重后的标题）
    const altTitles = (catalogIdentity(media).titles ?? []).filter(title => title.toLowerCase() !== searchQuery.toLowerCase())
    const context: MagnetSearchContext = {
      episode: targetEpisode, anilistId: media.id, altTitles,
      ...(media.year === undefined ? {} : { year: media.year }),
      ...(offsetValue === undefined ? {} : { absolute: targetEpisode + offsetValue }),
    }
    try {
      await streamPluginMagnets(searchQuery, context, event => {
        if (!live()) return
        if (event.event === 'scope') update(state => ({ ...state, scope: event.scope }))
        else if (event.event === 'item') update(state => ({ ...state, items: [...state.items, event.item] }))
        else if (event.event === 'outcome') update(state => ({ ...state, outcomes: [...state.outcomes, event.outcome] }))
        else update(state => ({ ...state, pending: false }))
      }, controller.signal)
    } catch (err) {
      if (!live()) return
      console.error('插件来源搜索失败', err)
      update(state => ({ ...state, pending: false, error: errorText(err, '插件来源搜索失败') }))
      return
    }
    if (live()) update(state => ({ ...state, pending: false }))
  }

  /**
   * 换集、重试、新搜索都走这里：旧的插件请求立刻作废并标成「仍在搜索」（查偏移可能要等几秒，
   * 别让界面先说「没有这一集」），偏移查好再问插件 —— 先知道有没有跨季连续编号，插件才知道
   * 要不要两种编号各问一次。
   */
  function queuePluginSearch(targetEpisode: number, searchQuery: string, offsetPromise: Promise<number | undefined>): void {
    const token = ++pluginRequest.current
    const version = requestVersion.current
    pluginAbort.current?.abort()
    setPlugin({ episode: targetEpisode, scope: null, items: [], outcomes: [], pending: true, error: null })
    void offsetPromise.then(offsetValue => {
      if (token === pluginRequest.current && version === requestVersion.current) void searchPlugin(targetEpisode, searchQuery, offsetValue)
    })
  }

  // 本机规则先回（不到一秒），列表立刻摆出来；插件的 BT 来源随后逐条追加，
  // 最慢的站点（Mikan 十几秒）不再拖住整个窗口。
  async function findReleases(targetEpisode: number): Promise<void> {
    const version = ++requestVersion.current
    pluginRequest.current++
    pluginAbort.current?.abort()
    // 搜的是哪一集就钉住哪一集：观看进度晚到时默认集会变，结果却还是这一集的
    setPicked(targetEpisode)
    const searchQuery = query.trim() || media.title
    setLocal({ phase: 'searching' })
    setPlugin(null)
    const offsetPromise = loadOffset()
    // 字幕组按各自习惯的名字登记发布（nyaa 多是原名、英文名）：用户没改搜索词时，本机规则也按
    // 目录里的几种写法各搜一次；改过就只搜用户写的
    const variants = searchQuery === media.title.trim()
      ? (catalogIdentity(media).titles ?? []).filter(title => title.toLowerCase() !== searchQuery.toLowerCase())
      : []
    try {
      const [result, sources, settings] = await Promise.all([
        variants.length > 0 ? searchMagnets(searchQuery, { altTitles: variants }) : searchMagnets(searchQuery),
        fetchSources(),
        fetchSettings(),
      ])
      if (version !== requestVersion.current) return
      setLocal({ phase: 'ready', query: searchQuery, result, sources, engineDown: !settings.torrent.enabled })
    } catch (err) {
      if (version === requestVersion.current) setLocal({ phase: 'error', message: errorText(err, '搜索磁力资源失败') })
      return
    }
    queuePluginSearch(targetEpisode, searchQuery, offsetPromise)
  }

  function selectEpisode(next: number): void {
    if (!Number.isSafeInteger(next) || next < 1 || next > MAX_EPISODE) return
    setPicked(next)
    if (local.phase !== 'ready') {
      void findReleases(next)
      return
    }
    // 本机规则的结果与集号无关；插件按作品搜回的也与集号无关（在途的照常收完），换集只重新排序。
    // 老插件按集号筛的结果要重问；按作品搜完却一条都没有时也借换集重问一次（失败的来源不缓存，等于重试）
    const wholeWork = plugin?.scope === 'all' && plugin.error === null && (plugin.pending || plugin.items.length > 0)
    if (wholeWork) {
      // 跨季编号上次没查到时借换集再查一次（查到了就是缓存，不发请求）
      void loadOffset()
      return
    }
    queuePluginSearch(next, local.query, loadOffset())
  }

  function retryPlugin(): void {
    if (local.phase === 'ready') queuePluginSearch(episode, local.query, loadOffset())
  }

  function submitSearch(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    void findReleases(episode)
  }

  function submitEpisode(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    selectEpisode(Number(episodeInput))
  }

  const offsetValue = offset.status === 'known' && offset.offset > 0 ? offset.offset : undefined
  // 标题没写季数时，只有确知没有前作才算第 1 季；说不准就不按季数筛
  const season = seasonOfTitle(media.title) ?? (offset.status === 'known' && offset.offset === 0 ? 1 : undefined)
  const extraKinds = extraKindsFor(media.format)
  // 本季集数：已知总集数与已播出集数取大（连载中的合集不会超过已播出的集数）
  const seasonTotal = Math.max(media.episodes ?? 0, airedEpisodeCount(media))
  const notBefore = notBeforeFor(media.startDate)
  const target: EpisodeTarget | null = isMovie ? null : {
    episode,
    ...(season === undefined ? {} : { season }),
    ...(offsetValue === undefined ? {} : { offset: offsetValue }),
    ...(seasonTotal > 0 ? { total: seasonTotal } : {}),
    ...(notBefore === undefined ? {} : { notBefore }),
    ...(extraKinds === undefined ? {} : { extraKinds }),
  }

  function start(item: SearchItem, button: HTMLButtonElement): void {
    // 有种子文件地址就优先走它（自带 info 与 tracker，不用等 DHT 找元数据；实测 8 秒 vs 18 秒），
    // 只有磁力的才走磁力
    const locator = item.torrentUrl ? { torrentUrl: item.torrentUrl } : { magnet: item.magnet }
    const request: TorrentPlayRequest = {
      ...locator,
      title: item.title,
      ...playbackHints(item, target),
      ...(typeof item.fileIndex === 'number' ? { suggestedFileIndex: item.fileIndex } : {}),
      ...catalogIdentity(media),
    }
    torrent.play(request, item.title, () => {
      if (button.isConnected && !button.disabled) button.focus()
      else trigger.current?.focus()
    })
    dialog.current?.close()
  }

  const aired = airedEpisodeCount(media)
  const choices = episodeChoices(media)
  const episodeTitle = media.episodeTitles?.find(item => item.episode === episode)?.title
  const offsetNote = offsetValue !== undefined
    ? `跨季连续编号：标题写第 ${episode + offsetValue} 集的发布也算这一集。`
    : offset.status === 'failed' ? '暂时查不到跨季编号，连续编号的发布可能没有排到前面，请在下方列表中查找。'
      : offset.status === 'unknown' ? '无法确认前作一共有几集，跨季连续编号的发布可能没有排到前面，请在下方列表中查找。' : null
  const items = local.phase === 'ready' ? [...local.result.items, ...(plugin?.items ?? [])] : []
  const outcomes = local.phase === 'ready' ? [...local.result.sources, ...(plugin?.outcomes ?? [])] : []

  return <>
    {!inline && <button type="button" className="discover-card-play discover-card-torrent" ref={trigger}
      aria-label={`按字幕组查找磁力：${media.title}`}
      onClick={() => { dialog.current?.showModal(); onOpenChange?.(true) }}>
      <Icon name="download" size={18} />字幕组
    </button>}
    <PlaybackSurface inline={inline} ref={dialog} className="media-play-dialog media-torrent-dialog" aria-label={`${media.title} 磁力资源`}
      onClose={() => {
        requestVersion.current += 1
        pluginAbort.current?.abort()
        // 没搜完就关了窗口：在途的结果都作废了。再打开时本机规则回到可搜索，插件来源说清楚只有
        // 一部分并给出重试 —— 而不是一直「搜索中」、按钮也点不了
        setLocal(state => (state.phase === 'searching' ? { phase: 'idle' } : state))
        setPlugin(state => (state?.pending ? { ...state, pending: false, error: '窗口关闭时插件来源还没搜完' } : state))
        trigger.current?.focus()
        onOpenChange?.(false)
      }}
      onClick={event => { if (event.target === dialog.current) dialog.current.close() }}>
      <div className="media-play-body">
        {!inline && <button type="button" className="icon-button media-play-close" aria-label="关闭磁力资源" onClick={() => dialog.current?.close()}><Icon name="close" /></button>}
        <p className="media-play-kicker">磁力边下边播</p>
        <h2>{inline ? '字幕组与版本' : media.title}</h2>
        <p className="media-play-hint">{isMovie ? '先选择字幕组，再挑选清晰度。合集会在播放前展开文件列表。' : '选好集数，每个字幕组里这一集的发布与含这一集的合集排在最前，其余发布照常列出。合集会在播放前自动定位到这一集，认不准时展开文件列表。'}</p>
        <form className="media-release-search" onSubmit={submitSearch}>
          <label className="media-torrent-query"><span>作品名称</span><input className="input" type="search" value={query} onChange={event => setQuery(event.target.value)} spellCheck={false} /></label>
          <button type="submit" className="btn btn--primary" disabled={local.phase === 'searching'}><Icon name="search" size={18} />{local.phase === 'searching' ? '搜索中…' : '查找字幕组'}</button>
        </form>
        {!isMovie && <section className="torrent-episode-picker" aria-labelledby={headingId}>
          <div className="media-play-selection"><h3 id={headingId}>选择集数</h3><span className="result result--dim">{aired > 0 ? `已播出 ${aired} 集` : '还没有已播出的集，可直接输入集号'}</span></div>
          {choices.length > 0 && <div className="torrent-episode-row" role="group" aria-label="集数">
            {choices.map(n => <button type="button" key={n} aria-pressed={n === episode} aria-label={`第 ${n} 集`}
              className={n === episode ? 'torrent-episode-chip torrent-episode-chip--active' : 'torrent-episode-chip'}
              title={media.episodeTitles?.find(item => item.episode === n)?.title} onClick={() => selectEpisode(n)}>{n}</button>)}
          </div>}
          {(choices.length === 0 || aired > choices.length) && <form className="torrent-episode-other" onSubmit={submitEpisode}>
            <label><span>{choices.length > 0 ? '其他集' : '集数'}</span><input className="input" type="number" min="1" max={MAX_EPISODE} step="1" required value={episodeInput} placeholder={String(episode)} onChange={event => setEpisodeInput(event.target.value)} /></label>
            <button type="submit" className="btn btn--sm">查找这一集</button>
          </form>}
          <p className="torrent-episode-note" role="status">第 {episode} 集{episodeTitle ? ` · ${episodeTitle}` : ''}{offsetNote ? ` · ${offsetNote}` : ''}</p>
        </section>}
        {local.phase === 'idle' && <div className="media-fansub-empty"><Icon name="download" size={32} /><h3>选择你喜欢的字幕版本</h3><p>{isMovie ? '搜索这部作品，按字幕组浏览全部发布。' : '搜索后按字幕组浏览全部发布，选中的这一集排在最前。'}</p></div>}
        {local.phase === 'searching' && <p className="result result--dim" role="status">正在查找字幕组与发布版本…</p>}
        {local.phase === 'error' && <p className="result result--err" role="alert">{local.message} <button type="button" className="link" onClick={() => void findReleases(episode)}>重试</button></p>}
        {local.phase === 'ready' && <TorrentReleaseResults key={`${episode}-${local.query}`} items={items} outcomes={outcomes} sources={local.sources}
          engineDown={local.engineDown} pluginPending={plugin?.pending ?? true} pluginError={plugin?.error ?? null}
          busy={torrent.busy} mediaId={media.id} {...(season === undefined ? {} : { wantedSeason: season })} target={target}
          onRetryPlugin={retryPlugin} onPlay={start} />}
      </div>
    </PlaybackSurface>
  </>
}
