import { PlaybackSurface } from './PlaybackSurface'
import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useTorrentPlayback } from '../torrent/TorrentPlayContext'
import { fetchSettings, fetchSources, searchMagnets, streamPluginMagnets } from '../../lib/endpoints'
import type { SearchItem, SearchResult, SourceOutcome, SourcesData } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { Icon } from '../ui/Icon'
import type { MediaSummary } from './types'
import './media-play.css'

const MAX_RESOURCE_RESULTS = 80

type ResourceState =
  | { phase: 'idle' }
  | { phase: 'searching'; episode: number }
  | { phase: 'ready'; episode: number; result: SearchResult; sources: SourcesData; engineDown: boolean; pluginPending: boolean }
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

/**
 * 目录作品的磁力入口：按作品搜索，先选字幕组，再选择该组发布的版本。
 * 作品 ID 不参与找源；它只提供名称与集数上下文，磁力始终来自用户配置的本机规则。
 */
export function MediaTorrentButton({ media, onOpenChange, inline = false }: { media: MediaSummary; onOpenChange?: (open: boolean) => void; inline?: boolean }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const requestVersion = useRef(0)
  const pluginAbort = useRef<AbortController | null>(null)
  const torrent = useTorrentPlayback()
  const [query, setQuery] = useState(media.title)
  const [state, setState] = useState<ResourceState>({ phase: 'idle' })

  useEffect(() => () => { requestVersion.current += 1; pluginAbort.current?.abort() }, [])
  useEffect(() => {
    setQuery(media.title)
    requestVersion.current++
    pluginAbort.current?.abort()
    setState({ phase: 'idle' })
  }, [media.id, media.title])

  async function findReleases(): Promise<void> {
    // 插件协议仍要求进度上下文；界面展示整部作品的全部发布，不按此集号筛选。
    const episode = Math.max(1, media.watched + 1)
    pluginAbort.current?.abort()
    const searchQuery = query.trim() || media.title
    const version = ++requestVersion.current
    setState({ phase: 'searching', episode })
    try {
      const altTitles = [media.titleNative, media.titleEnglish]
        .filter((title): title is string => typeof title === 'string' && title.trim() !== '' && title.trim() !== searchQuery)
      const context = { episode, anilistId: media.id, ...(media.year === undefined ? {} : { year: media.year }), altTitles }
      // 本机规则先回（不到一秒），列表立刻摆出来；插件的 BT 来源随后逐条追加，
      // 最慢的站点（Mikan 十几秒）不再拖住整个窗口。
      const [result, sources, settings] = await Promise.all([
        searchMagnets(searchQuery),
        fetchSources(),
        fetchSettings(),
      ])
      if (version !== requestVersion.current) return
      setState({ phase: 'ready', episode, result, sources, engineDown: !settings.torrent.enabled, pluginPending: true })
      const controller = new AbortController()
      pluginAbort.current?.abort()
      pluginAbort.current = controller
      try {
        await streamPluginMagnets(searchQuery, context, event => {
          if (version !== requestVersion.current) return
          setState(current => {
            if (current.phase !== 'ready' || current.episode !== episode) return current
            if (event.event === 'item') return { ...current, result: { ...current.result, items: [...current.result.items, event.item] } }
            if (event.event === 'outcome') return { ...current, result: { ...current.result, sources: [...current.result.sources, event.outcome] } }
            return { ...current, pluginPending: false }
          })
        }, controller.signal)
      } catch (err) {
        if (controller.signal.aborted) return
        console.error('插件来源搜索失败', err)
      }
      if (version === requestVersion.current) setState(current => (current.phase === 'ready' ? { ...current, pluginPending: false } : current))
    } catch (err) {
      if (version === requestVersion.current) setState({ phase: 'error', episode, message: errorText(err, '搜索磁力资源失败') })
    }
  }

  function submitManual(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    void findReleases()
  }

  function start(item: SearchItem, button: HTMLButtonElement): void {
    // 有种子文件地址就优先走它（自带 info 与 tracker，不用等 DHT 找元数据；实测 8 秒 vs 18 秒），
    // 只有磁力的才走磁力
    const target = item.torrentUrl ? { torrentUrl: item.torrentUrl } : { magnet: item.magnet }
    const episode = item.episode
    const episodeHint = typeof episode === 'number' && Number.isFinite(episode) && episode > 0 && (item.kind === undefined || item.kind === 'main' || item.kind === 'movie') ? episode : undefined
    torrent.play(
      { ...target, title: item.title, ...(episodeHint === undefined ? {} : { episodeHint }) },
      item.title,
      () => {
        if (button.isConnected && !button.disabled) button.focus()
        else trigger.current?.focus()
      },
    )
    dialog.current?.close()
  }

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
        trigger.current?.focus()
        onOpenChange?.(false)
      }}
      onClick={event => { if (event.target === dialog.current) dialog.current.close() }}>
      <div className="media-play-body">
        {!inline && <button type="button" className="icon-button media-play-close" aria-label="关闭磁力资源" onClick={() => dialog.current?.close()}><Icon name="close" /></button>}
        <p className="media-play-kicker">磁力边下边播</p>
        <h2>{inline ? '字幕组与版本' : media.title}</h2>
        <p className="media-play-hint">先选择字幕组，再挑选清晰度、单集或整季合集。合集会在播放前展开文件列表。</p>
        <form className="media-release-search" onSubmit={submitManual}>
          <label className="media-torrent-query"><span>作品名称</span><input className="input" type="search" value={query} onChange={event => setQuery(event.target.value)} spellCheck={false} /></label>
          <button type="submit" className="btn btn--primary" disabled={state.phase === 'searching'}><Icon name="search" size={18} />{state.phase === 'searching' ? '搜索中…' : '查找字幕组'}</button>
        </form>
        {state.phase === 'idle' && <div className="media-fansub-empty"><Icon name="download" size={32} /><h3>选择你喜欢的字幕版本</h3><p>搜索这部作品，按字幕组浏览全部发布。</p></div>}
        {state.phase === 'searching' && <p className="result result--dim" role="status">正在查找字幕组与发布版本…</p>}
        {state.phase === 'error' && <p className="result result--err" role="alert">{state.message} <button type="button" className="link" onClick={() => void findReleases()}>重试</button></p>}
        {state.phase === 'ready' && <ResourceResults key={`${state.episode}-${state.result.query}`} state={state} busy={torrent.busy} mediaId={media.id} mediaTitle={media.title} onPlay={start} />}
      </div>
    </PlaybackSurface>
  </>
}

const UNGROUPED = '未分类'

/** 每部作品记住用户上次选的字幕组：下次打开直接排在最前并预选。 */
function fansubMemoryKey(mediaId: number): string { return `nagare:fansub:${mediaId}` }
export function rememberedFansub(mediaId: number): string | null {
  try { return localStorage.getItem(fansubMemoryKey(mediaId)) } catch { return null }
}
function rememberFansub(mediaId: number, group: string): void {
  try { localStorage.setItem(fansubMemoryKey(mediaId), group) } catch { /* 无存储时只是不记住 */ }
}

interface FansubGroup { name: string; items: SearchItem[] }

/** 合集 / 整季 BD 包：老番往往只剩这种还有人做种，播放时会弹选集 */
export function isBatchRelease(item: SearchItem): boolean {
  return typeof item.episode !== 'number' && /合集|全\s*\d+\s*[集话話]|\bBatch\b|BD-?(?:Rip|Box|MV)|BDRip|\[\s*\d{1,3}\s*[-~]\s*\d{1,3}\s*(?:Fin|END)?\s*\]|\d{1,3}\s*-\s*\d{1,3}\s*(?:Fin|END)\b/i.test(item.title)
}

/** 同一个种子会从多个来源（Anime Garden / 動漫花園 / Mikan…）各来一次，按 infohash 折叠。 */
export function releaseKey(item: SearchItem): string {
  const hash = item.infohash?.toLowerCase() || /btih:([0-9a-z]{32,40})/i.exec(item.magnet)?.[1]?.toLowerCase()
  return hash ?? (item.torrentUrl ?? item.magnet ?? item.title).toLowerCase()
}

/**
 * 目录作品自己是第几季：标题里的「第二季 / S2 / II」等；没写就是第 1 季。
 * 续作命名不统一（「TO THE TOP」这类不带数字）时解不出来，只能靠年份排序兜底。
 */
export function seasonOfTitle(title: string): number | undefined {
  const t = title.normalize('NFKC')
  const cn = /第\s*([一二三四五六七八九十\d]+)\s*[季期部]/.exec(t)
  if (cn) return chineseNumber(cn[1]!)
  const en = /(?:\bS|Season\s*)(\d{1,2})\b/i.exec(t)
  if (en) return Number(en[1])
  const roman = /\b(II|III|IV|V|VI)\b|\s(Ⅱ|Ⅲ|Ⅳ)\s*$/.exec(t)
  if (roman) return { II: 2, III: 3, IV: 4, V: 5, VI: 6, 'Ⅱ': 2, 'Ⅲ': 3, 'Ⅳ': 4 }[(roman[1] ?? roman[2])!]
  const nth = /(\d)(?:st|nd|rd|th)\s+Season/i.exec(t)
  if (nth) return Number(nth[1])
  return undefined
}

function chineseNumber(raw: string): number | undefined {
  if (/^\d+$/.test(raw)) return Number(raw)
  const digits: Record<string, number> = { 一: 1, 二: 2, 三: 3, 四: 4, 五: 5, 六: 6, 七: 7, 八: 8, 九: 9 }
  if (raw === '十') return 10
  if (raw.startsWith('十')) return 10 + (digits[raw[1]!] ?? 0)
  if (raw.endsWith('十')) return (digits[raw[0]!] ?? 0) * 10
  return digits[raw]
}

export interface FansubGroupingOptions {
  /** 目录作品的季数；缺省按标题解析，解不出按第 1 季 */
  wantedSeason?: number
}

export function groupByFansub(items: SearchItem[], remembered: string | null, options: FansubGroupingOptions = {}): FansubGroup[] {
  const wantedSeason = options.wantedSeason ?? 1
  const byName = new Map<string, FansubGroup>()
  const seen = new Map<string, SearchItem>()
  const deduped: SearchItem[] = []
  for (const item of items) {
    const key = releaseKey(item)
    const existing = seen.get(key)
    if (existing === undefined) {
      seen.set(key, item)
      deduped.push(item)
    } else if (typeof item.seeders === 'number' && typeof existing.seeders !== 'number') {
      // 同一种子以带做种数的那条为准（排序靠它）
      deduped[deduped.indexOf(existing)] = { ...item, group: existing.group ?? item.group }
      seen.set(key, item)
    }
  }
  for (const item of deduped) {
    const name = item.group?.trim() || item.fansub?.trim() || UNGROUPED
    const group = byName.get(name) ?? { name, items: [] }
    group.items.push(item)
    byName.set(name, group)
  }
  const seeders = (item: SearchItem) => typeof item.seeders === 'number' ? item.seeders : -1
  const currentSeason = (item: SearchItem) => (item.season ?? 1) === wantedSeason
  for (const group of byName.values()) {
    group.items.sort((a, b) => Number(currentSeason(b)) - Number(currentSeason(a)) || Number(isBatchRelease(b)) - Number(isBatchRelease(a)) || seeders(b) - seeders(a) || (Date.parse(b.date ?? '') || 0) - (Date.parse(a.date ?? '') || 0))
  }
  return [...byName.values()].sort((a, b) => Number(b.name === remembered) - Number(a.name === remembered) || b.items.length - a.items.length || a.name.localeCompare(b.name, 'zh'))
}

function ResourceResults({ state, busy, mediaId, mediaTitle, onPlay }: {
  state: Extract<ResourceState, { phase: 'ready' }>
  busy: boolean
  mediaId: number
  mediaTitle: string
  onPlay: (item: SearchItem, button: HTMLButtonElement) => void
}) {
  const remembered = rememberedFansub(mediaId)
  const groups = groupByFansub(state.result.items, remembered, { wantedSeason: seasonOfTitle(mediaTitle) ?? 1 })
  const [chosen, setChosen] = useState<string | null>(null)
  const active = groups.find(group => group.name === chosen) ?? groups[0] ?? null

  // 本机规则和插件 BT 来源都没有时才算「没配源」；插件给了结果就照常展示
  if (state.sources.sources.length === 0 && state.result.sources.length === 0 && !state.pluginPending) return <div className="media-resource-empty">
    <p className="result result--warn">尚未配置资源源：启用本地来源插件（Nagare Source）或添加规则仓库。</p>
    <a className="btn btn--sm" href="/settings#sources">去设置</a>
  </div>

  const trouble = state.result.sources.filter(outcome => outcome.state === 'dead' || outcome.state === 'failed')
  const sourceNames = new Map(state.sources.sources.map(source => [source.id, source.name]))
  const play = (item: SearchItem, button: HTMLButtonElement, group: string) => {
    if (group !== UNGROUPED) rememberFansub(mediaId, group)
    onPlay(item, button)
  }
  const sourceLabel = (item: SearchItem) => sourceNames.get(item.source) ?? (item.source.startsWith('plugin:') ? `插件 · ${item.provider ?? item.source.slice('plugin:'.length)}` : item.source)
  const itemMeta = (item: SearchItem) => [item.resolution, item.size, typeof item.seeders === 'number' ? `做种 ${item.seeders}` : null, publishedLabel(item.date), sourceLabel(item)].filter(Boolean).join(' · ')
  const playButton = (item: SearchItem, group: string, label = '播放') => <button type="button" className="btn btn--sm btn--primary" disabled={busy || state.engineDown}
    title={busy ? '已有磁力任务，请先停止底部状态条中的任务' : undefined}
    onClick={event => play(item, event.currentTarget, group)}>{label}</button>

  const rowLabel = (item: SearchItem) => [
    typeof item.episode === 'number' ? `第 ${item.episode} 集` : isBatchRelease(item) ? '整季合集 · 播放前选文件' : '播放前选择文件',
    typeof item.season === 'number' ? `第 ${item.season} 季` : null,
    item.kind && item.kind !== 'main' ? item.kind.toUpperCase() : null,
    itemMeta(item),
  ].filter(Boolean).join(' · ')

  return <section className="media-resource-results" aria-label="按字幕组浏览磁力资源">
    <div className="media-play-selection"><h3>字幕组</h3><span className="result result--dim" role="status">{groups.length} 个分类 · {groups.reduce((n, group) => n + group.items.length, 0)} 个版本{state.pluginPending ? ' · 插件来源仍在搜索…' : ''}</span></div>
    {state.engineDown && <p className="result result--err" role="alert">磁力引擎不可用。<a className="link" href="/settings#torrent">查看设置</a></p>}
    {trouble.length > 0 && <p className="result result--warn" role="status">{sourceTrouble(trouble)}</p>}
    {state.result.items.length === 0 ? <p className="media-play-hint" role="status">{state.pluginPending ? '本机规则没有结果，插件来源仍在搜索…' : '没有找到发布版本。可以修改作品名称后重试。'}</p> : <div className="media-fansub-browser">
      <div className="media-fansub-row" aria-label="选择字幕组">
        {groups.map(group => <button type="button" key={group.name} aria-pressed={active?.name === group.name}
          className={active?.name === group.name ? 'media-fansub-chip media-fansub-chip--active' : 'media-fansub-chip'}
          aria-label={`字幕组 ${group.name}`} onClick={() => setChosen(group.name)}>
          <span className="media-fansub-avatar" aria-hidden="true">{group.name.slice(0, 2)}</span><strong>{group.name}</strong>
          <span>{group.items.length} 个版本{group.items.some(isBatchRelease) ? ' · 含合集' : ''}{group.name === remembered ? ' · 上次选择' : ''}</span>
        </button>)}
      </div>
      {active && <div className="media-fansub-detail" aria-label={`${active.name} 的资源`}>
        <div className="media-fansub-heading"><h3>{active.name}</h3><span>{[...new Set(active.items.map(item => item.resolution).filter(Boolean))].join(' / ')}</span></div>
        <ul className="media-resource-list">
          {active.items.slice(0, MAX_RESOURCE_RESULTS).map(item => <li key={releaseKey(item)}>
            <div><strong>{item.title}</strong><span>{rowLabel(item)}</span></div>
            {playButton(item, active.name, isBatchRelease(item) || item.episode == null ? '选择文件' : '播放')}
          </li>)}
        </ul>
        {active.items.length > MAX_RESOURCE_RESULTS && <p className="media-play-hint">仅显示前 {MAX_RESOURCE_RESULTS} 个版本，请细化作品名称继续搜索。</p>}
      </div>}
    </div>}
  </section>
}

/** 发布日期只显示到月；老发布（两年以上）标出来，提醒用户可能已经没人做种。 */
export function publishedLabel(date: string | null | undefined): string | null {
  const ms = Date.parse(date ?? '')
  if (!ms) return null
  const d = new Date(ms)
  const label = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
  return Date.now() - ms > 2 * 365 * 24 * 3600 * 1000 ? `${label} 发布（较旧，可能无人做种）` : `${label} 发布`
}

function sourceTrouble(outcomes: SourceOutcome[]): string {
  const dead = outcomes.filter(outcome => outcome.state === 'dead').length
  const failed = outcomes.filter(outcome => outcome.state === 'failed').length
  return [dead ? `${dead} 个源规则异常` : '', failed ? `${failed} 个源连接失败` : ''].filter(Boolean).join('，')
}
