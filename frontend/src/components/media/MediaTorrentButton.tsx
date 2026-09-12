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
      const altTitles = [media.titleNative, media.titleEnglish]
        .filter((title): title is string => typeof title === 'string' && title.trim() !== '' && title.trim() !== searchQuery)
      const [result, sources, settings] = await Promise.all([
        searchMagnets(searchQuery, { episode, anilistId: media.id, ...(media.year === undefined ? {} : { year: media.year }), altTitles }),
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
    // acg.rip 一类只给 .torrent 地址：走种子文件路径（自带 info 与 tracker，不用等元数据）
    const target = item.magnet ? { magnet: item.magnet } : { torrentUrl: item.torrentUrl ?? '' }
    torrent.play(
      { ...target, title: item.title, episodeHint: episode },
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
        {state.phase === 'ready' && <ResourceResults key={`${state.episode}-${state.result.query}`} state={state} busy={torrent.busy} mediaId={media.id} onPlay={start} />}
      </div>
    </dialog>
  </>
}

const UNGROUPED = '未标注字幕组'

/** 每部作品记住用户上次选的字幕组：下次打开直接排在最前并预选。 */
function fansubMemoryKey(mediaId: number): string { return `nagare:fansub:${mediaId}` }
export function rememberedFansub(mediaId: number): string | null {
  try { return localStorage.getItem(fansubMemoryKey(mediaId)) } catch { return null }
}
function rememberFansub(mediaId: number, group: string): void {
  try { localStorage.setItem(fansubMemoryKey(mediaId), group) } catch { /* 无存储时只是不记住 */ }
}

interface FansubGroup {
  name: string
  /** 命中目标集的正片 */
  hits: SearchItem[]
  /** 该字幕组的其余条目（别的集、合集、特典、未识别） */
  others: SearchItem[]
}

/**
 * 按字幕组分组，命中目标集的组排前面；同为命中/未命中时，上次选过的组优先，再按命中数多、名称序。
 * 组内命中条目按做种数降序（无做种数视为 -1），让「播放这一组」拿到的就是第一条。
 */
/** 同一个种子会从多个来源（Anime Garden / 動漫花園 / Mikan…）各来一次，按 infohash 折叠。 */
export function releaseKey(item: SearchItem): string {
  const hash = item.infohash?.toLowerCase() || /btih:([0-9a-z]{32,40})/i.exec(item.magnet)?.[1]?.toLowerCase()
  return hash ?? (item.torrentUrl ?? item.magnet ?? item.title).toLowerCase()
}

export function groupByFansub(items: SearchItem[], episode: number, remembered: string | null): FansubGroup[] {
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
    const group = byName.get(name) ?? { name, hits: [], others: [] }
    const isHit = item.episode === episode && (item.kind ?? 'main') === 'main'
    ;(isHit ? group.hits : group.others).push(item)
    byName.set(name, group)
  }
  // 做种数优先；来源不给做种数（Anime Garden 就不给）时按发布时间新的在前——
  // 几年前的冷门发布多半已经没人做种了，先试新的。
  const seeders = (item: SearchItem) => (typeof item.seeders === 'number' ? item.seeders : -1)
  const published = (item: SearchItem) => Date.parse(item.date ?? '') || 0
  for (const group of byName.values()) group.hits.sort((a, b) => seeders(b) - seeders(a) || published(b) - published(a))
  return [...byName.values()].sort((a, b) =>
    Number(b.hits.length > 0) - Number(a.hits.length > 0) ||
    Number(b.name === remembered) - Number(a.name === remembered) ||
    b.hits.length - a.hits.length ||
    a.name.localeCompare(b.name, 'zh'),
  )
}

function ResourceResults({ state, busy, mediaId, onPlay }: {
  state: Extract<ResourceState, { phase: 'ready' }>
  busy: boolean
  mediaId: number
  onPlay: (item: SearchItem, episode: number, button: HTMLButtonElement) => void
}) {
  const remembered = rememberedFansub(mediaId)
  const groups = groupByFansub(state.result.items, state.episode, remembered)
  const hitGroups = groups.filter(group => group.hits.length > 0)
  const [chosen, setChosen] = useState<string | null>(null)
  const active = groups.find(group => group.name === chosen) ?? hitGroups[0] ?? groups[0] ?? null

  // 本机规则和插件 BT 来源都没有时才算「没配源」；插件给了结果就照常展示
  if (state.sources.sources.length === 0 && state.result.sources.length === 0) return <div className="media-resource-empty">
    <p className="result result--warn">尚未配置资源源：启用本地来源插件（Nagare Source）或添加规则仓库。</p>
    <a className="btn btn--sm" href="/settings#sources">去设置</a>
  </div>

  const trouble = state.result.sources.filter(outcome => outcome.state === 'dead' || outcome.state === 'failed')
  const sourceNames = new Map(state.sources.sources.map(source => [source.id, source.name]))
  const play = (item: SearchItem, button: HTMLButtonElement, group: string) => {
    if (group !== UNGROUPED) rememberFansub(mediaId, group)
    onPlay(item, state.episode, button)
  }
  const sourceLabel = (item: SearchItem) => sourceNames.get(item.source) ?? (item.source.startsWith('plugin:') ? `插件 · ${item.provider ?? item.source.slice('plugin:'.length)}` : item.source)
  const itemMeta = (item: SearchItem) => [item.resolution, item.size, typeof item.seeders === 'number' ? `做种 ${item.seeders}` : null, publishedLabel(item.date), sourceLabel(item)].filter(Boolean).join(' · ')
  const playButton = (item: SearchItem, group: string, label = '播放') => <button type="button" className="btn btn--sm btn--primary" disabled={busy || state.engineDown}
    title={busy ? '已有磁力任务，请先停止底部状态条中的任务' : undefined}
    onClick={event => play(item, event.currentTarget, group)}>{label}</button>

  return <section className="media-resource-results" aria-label={`第 ${state.episode} 集磁力资源`}>
    <div className="media-play-selection"><h3>第 {state.episode} 集资源</h3><span className="result result--dim">{hitGroups.length} 个字幕组命中 · 共 {state.result.items.length} 条</span></div>
    {state.engineDown && <p className="result result--err" role="alert">磁力引擎不可用。<a className="link" href="/settings#torrent">查看设置</a></p>}
    {trouble.length > 0 && <p className="result result--warn" role="status">{sourceTrouble(trouble)}</p>}
    {state.result.items.length === 0 ? <p className="media-play-hint">启用的源没有返回结果。可以修改搜索词后重试；源异常不等于这部作品没有资源。</p> : <>
      <div className="media-fansub-row" role="tablist" aria-label="按字幕组选择">
        {groups.map(group => <button type="button" key={group.name} role="tab" aria-selected={active?.name === group.name}
          className={active?.name === group.name ? 'media-fansub-chip media-fansub-chip--active' : 'media-fansub-chip'}
          aria-label={`字幕组 ${group.name}`} onClick={() => setChosen(group.name)}>
          <strong>{group.name}</strong>
          <span>{group.hits.length > 0 ? `第 ${state.episode} 集 · ${group.hits[0]!.resolution ?? ''}`.replace(/ · $/, '') : `无第 ${state.episode} 集`}{group.name === remembered ? ' · 上次' : ''}</span>
        </button>)}
      </div>
      {hitGroups.length === 0 && <p className="media-play-hint">没有识别到第 {state.episode} 集的正片条目，可能标题写法特殊或尚未发布；下面是各字幕组的全部结果。</p>}
      {active && <div className="media-fansub-detail" aria-label={`${active.name} 的资源`}>
        {active.hits.length > 0 && <ul className="media-resource-list">{active.hits.map((item, index) => <li key={`hit-${item.source}-${index}`}>
          <div><strong>{item.title}</strong><span>{itemMeta(item)}</span></div>
          {playButton(item, active.name, index === 0 ? `播放第 ${state.episode} 集` : '播放')}
        </li>)}</ul>}
        {active.others.length > 0 && <details className="media-fansub-others" open={active.hits.length === 0}>
          <summary>{active.name} 的其他条目（{active.others.length}）</summary>
          <ul className="media-resource-list">{active.others.slice(0, MAX_RESOURCE_RESULTS).map((item, index) => <li key={`other-${item.source}-${index}`}>
            <div><strong>{item.title}</strong><span>{[typeof item.episode === 'number' ? `第 ${item.episode} 集` : '未识别集数', itemMeta(item)].join(' · ')}</span></div>
            {playButton(item, active.name)}
          </li>)}</ul>
        </details>}
      </div>}
    </>}
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
