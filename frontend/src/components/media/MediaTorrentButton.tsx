import { PlaybackSurface } from './PlaybackSurface'
import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useTorrentPlayback } from '../torrent/TorrentPlayContext'
import { fetchSettings, fetchSources, searchMagnets, streamPluginMagnets } from '../../lib/endpoints'
import type { MagnetSearchContext, SearchItem, SearchResult, SourceOutcome, SourcesData, TorrentPlayRequest } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { Icon } from '../ui/Icon'
import { catalogIdentity, defaultTorrentEpisode, extraKindsFor, playbackHints, seasonOfTitle } from './releaseEpisodes'
import { TorrentReleaseResults } from './TorrentReleaseResults'
import type { MediaSummary } from './types'
import './media-play.css'

/** 本机规则的结果（规则按作品名搜） */
type LocalState =
  | { phase: 'idle' }
  | { phase: 'searching' }
  | { phase: 'ready'; query: string; result: SearchResult; sources: SourcesData; engineDown: boolean }
  | { phase: 'error'; message: string }

/**
 * 插件 BT 来源的结果。scope 由插件流的首行告知：all 是按作品搜回的全部发布；episode 是只会按集号
 * 问的老插件，结果只有请求的那一集；null 是还没收到首行。
 */
interface PluginState {
  /** 第几次插件请求：只用来丢弃过期的流 */
  token: number
  /** 问老插件时用的集号（scope=episode 时结果只有这一集） */
  episode: number
  scope: 'all' | 'episode' | null
  items: SearchItem[]
  outcomes: SourceOutcome[]
  pending: boolean
  error: string | null
}

/**
 * 目录作品的磁力入口：按作品名搜回全部发布，直接按字幕组分类浏览，不先选集数。
 * 单集发布按条目自己的集号直接播放；合集在播放前展开文件列表，由用户挑要看的那一集。
 * 作品 ID 不参与找源：磁力始终来自用户自己装的插件与本机规则；ID 只随播放请求留在本机，
 * 用来校验弹幕匹配没有认成别的作品，从不拿它去 animego 换磁力。
 */
export function MediaTorrentButton({ media, onOpenChange, inline = false }: { media: MediaSummary; onOpenChange?: (open: boolean) => void; inline?: boolean }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const requestVersion = useRef(0)
  const pluginAbort = useRef<AbortController | null>(null)
  const pluginRequest = useRef(0)
  const torrent = useTorrentPlayback()
  const isMovie = media.format === 'MOVIE'
  const [query, setQuery] = useState(media.title)
  const [local, setLocal] = useState<LocalState>({ phase: 'idle' })
  const [plugin, setPlugin] = useState<PluginState | null>(null)
  // 只会按集号问的老插件需要一个集号：给它下一集没看的。按作品搜的插件用不上
  const legacyEpisode = defaultTorrentEpisode(media)

  useEffect(() => () => { requestVersion.current += 1; pluginAbort.current?.abort() }, [])
  useEffect(() => {
    setQuery(media.title)
    requestVersion.current++
    pluginAbort.current?.abort()
    setLocal({ phase: 'idle' })
    setPlugin(null)
    // 作品页上切到这个标签就直接按字幕组列出来，不用再点一次搜索
    if (inline) void findReleases(media.title.trim() || media.title)
  }, [media.id, media.title])

  async function searchPlugin(searchQuery: string): Promise<void> {
    pluginAbort.current?.abort()
    const controller = new AbortController()
    pluginAbort.current = controller
    const token = ++pluginRequest.current
    const live = () => pluginAbort.current === controller && !controller.signal.aborted
    const update = (change: (state: PluginState) => PluginState) => setPlugin(state => (state?.token === token ? change(state) : state))
    setPlugin({ token, episode: legacyEpisode, scope: null, items: [], outcomes: [], pending: true, error: null })
    // 插件按作品身份找：目录里的其他写法照带（与本机规则同一份去重后的标题）
    const altTitles = (catalogIdentity(media).titles ?? []).filter(title => title.toLowerCase() !== searchQuery.toLowerCase())
    const context: MagnetSearchContext = {
      episode: legacyEpisode, anilistId: media.id, altTitles,
      ...(media.year === undefined ? {} : { year: media.year }),
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

  // 本机规则先回（不到一秒），列表立刻摆出来；插件的 BT 来源随后逐条追加，
  // 最慢的站点（Mikan 十几秒）不再拖住整个面板。
  async function findReleases(searchQuery: string): Promise<void> {
    const version = ++requestVersion.current
    pluginRequest.current++
    pluginAbort.current?.abort()
    setLocal({ phase: 'searching' })
    setPlugin(null)
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
    void searchPlugin(searchQuery)
  }

  function currentQuery(): string {
    return query.trim() || media.title
  }

  function retryPlugin(): void {
    if (local.phase === 'ready') void searchPlugin(local.query)
  }

  function submitSearch(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    void findReleases(currentQuery())
  }

  function open(): void {
    dialog.current?.showModal()
    onOpenChange?.(true)
    // 打开就直接按字幕组列出来；上次已经搜过（或搜到一半关了窗口）就保留原样
    if (local.phase === 'idle') void findReleases(currentQuery())
  }

  const season = seasonOfTitle(media.title)
  const extraKinds = extraKindsFor(media.format)

  function start(item: SearchItem, button: HTMLButtonElement): void {
    // 有种子文件地址就优先走它（自带 info 与 tracker，不用等 DHT 找元数据；实测 8 秒 vs 18 秒），
    // 只有磁力的才走磁力
    const locator = item.torrentUrl ? { torrentUrl: item.torrentUrl } : { magnet: item.magnet }
    const request: TorrentPlayRequest = {
      ...locator,
      title: item.title,
      ...playbackHints(item, extraKinds),
      ...(typeof item.fileIndex === 'number' ? { suggestedFileIndex: item.fileIndex } : {}),
      ...catalogIdentity(media),
    }
    torrent.play(request, item.title, () => {
      if (button.isConnected && !button.disabled) button.focus()
      else trigger.current?.focus()
    })
    dialog.current?.close()
  }

  const items = local.phase === 'ready' ? [...local.result.items, ...(plugin?.items ?? [])] : []
  const outcomes = local.phase === 'ready' ? [...local.result.sources, ...(plugin?.outcomes ?? [])] : []

  return <>
    {!inline && <button type="button" className="discover-card-play discover-card-torrent" ref={trigger}
      aria-label={`按字幕组查找磁力：${media.title}`}
      onClick={open}>
      <Icon name="download" size={18} />字幕组
    </button>}
    <PlaybackSurface inline={inline} ref={dialog} className="media-play-dialog media-torrent-dialog" aria-label={`${media.title} 磁力资源`}
      onClose={() => {
        requestVersion.current += 1
        pluginAbort.current?.abort()
        // 没搜完就关了窗口：在途的结果都作废了。再打开时本机规则重新搜，插件来源说清楚只有
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
        <p className="media-play-hint">{isMovie ? '按字幕组列出这部作品的全部发布，选好字幕组再挑清晰度。' : '按字幕组列出这部作品的全部发布：合集在前，单集按集号排列。单集直接播放，合集在播放前展开文件列表挑要看的那一集。'}</p>
        <form className="media-release-search" onSubmit={submitSearch}>
          <label className="media-torrent-query"><span>作品名称</span><input className="input" type="search" value={query} onChange={event => setQuery(event.target.value)} spellCheck={false} /></label>
          <button type="submit" className="btn btn--primary" disabled={local.phase === 'searching'}><Icon name="search" size={18} />{local.phase === 'searching' ? '搜索中…' : local.phase === 'idle' ? '查找字幕组' : '重新搜索'}</button>
        </form>
        {plugin?.scope === 'episode' && <p className="media-play-hint" role="status">当前的 Nagare Source 只会按集号搜索，插件来源只列出了第 {plugin.episode} 集的发布；升级插件后可列出整部作品。</p>}
        {local.phase === 'searching' && <p className="result result--dim" role="status">正在查找字幕组与发布版本…</p>}
        {local.phase === 'error' && <p className="result result--err" role="alert">{local.message} <button type="button" className="link" onClick={() => void findReleases(currentQuery())}>重试</button></p>}
        {local.phase === 'ready' && <TorrentReleaseResults key={local.query} items={items} outcomes={outcomes} sources={local.sources}
          engineDown={local.engineDown} pluginPending={plugin?.pending ?? true} pluginError={plugin?.error ?? null}
          busy={torrent.busy} mediaId={media.id} {...(season === undefined ? {} : { wantedSeason: season })}
          onRetryPlugin={retryPlugin} onPlay={start} />}
      </div>
    </PlaybackSurface>
  </>
}
