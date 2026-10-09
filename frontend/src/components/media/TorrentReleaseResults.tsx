import { useEffect, useState } from 'react'
import type { SearchItem, SourceOutcome, SourcesData } from '../../lib/endpoints'
import { parsePublished } from '../../lib/format'
import { episodeFit, fitRank, isBatchRelease, matchesEpisode } from './releaseEpisodes'
import type { EpisodeTarget } from './releaseEpisodes'

const MAX_RESOURCE_RESULTS = 80
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

/**
 * 同一个种子从多个来源各来一次时留哪一条：照 animego —— 有做种数的胜过没有的，做种多的胜过少的，
 * 再按来源优先级（AnimeTosho > Anime Garden > ACG.RIP > Nyaa > 動漫花園 > Mikan）。
 * 本机规则与插件的来源 id 同名（插件的带 plugin: 前缀），一起比。
 */
const SOURCE_SCORE = new Map([['tosho', 6], ['garden', 5], ['acg', 4], ['nyaa', 3], ['dmhy', 2], ['mikan', 1]])
const sourceScore = (item: SearchItem) => SOURCE_SCORE.get(item.source.replace(/^plugin:/, '')) ?? 0

function betterRelease(candidate: SearchItem, incumbent: SearchItem): boolean {
  const a = typeof candidate.seeders === 'number' ? candidate.seeders : null
  const b = typeof incumbent.seeders === 'number' ? incumbent.seeders : null
  if (a !== null && b !== null && a !== b) return a > b
  if ((a === null) !== (b === null)) return a !== null
  return sourceScore(candidate) > sourceScore(incumbent)
}

/** 同一个种子会从多个来源（Anime Garden / 動漫花園 / Mikan…）各来一次，按 infohash 折叠。 */
export function releaseKey(item: SearchItem): string {
  const hash = item.infohash?.toLowerCase() || /btih:([0-9a-z]{32,40})/i.exec(item.magnet)?.[1]?.toLowerCase()
  return hash ?? (item.torrentUrl ?? item.magnet ?? item.title).toLowerCase()
}

export interface FansubGroupingOptions {
  /** 目录作品的季数；说不准时缺省，组内就不按季数排 */
  wantedSeason?: number
  /** 选定的集：给了就把有这一集的字幕组排在前面，组内按「这一集的单集 → 含这一集的合集 → 说不准的合集 → 别的」排 */
  target?: EpisodeTarget
}

export function groupByFansub(items: SearchItem[], remembered: string | null, options: FansubGroupingOptions = {}): FansubGroup[] {
  const { wantedSeason, target } = options
  const byName = new Map<string, FansubGroup>()
  // 键 → 在 deduped 里的位置（换成更好的那一条时原地替换，不改变先来后到）
  const seen = new Map<string, number>()
  const deduped: SearchItem[] = []
  for (const item of items) {
    const key = releaseKey(item)
    const index = seen.get(key)
    if (index === undefined) {
      seen.set(key, deduped.length)
      deduped.push(item)
      continue
    }
    const existing = deduped[index]!
    if (betterRelease(item, existing)) {
      // 字幕组名与插件指明的文件下标不能在折叠时丢掉
      deduped[index] = { ...item, group: existing.group ?? item.group, fileIndex: item.fileIndex ?? existing.fileIndex }
    }
  }
  for (const item of deduped) {
    const name = item.group?.trim() || item.fansub?.trim() || UNGROUPED
    const group = byName.get(name) ?? { name, items: [] }
    group.items.push(item)
    byName.set(name, group)
  }
  const seeders = (item: SearchItem) => typeof item.seeders === 'number' ? item.seeders : -1
  const currentSeason = (item: SearchItem) => wantedSeason !== undefined && (item.season ?? 1) === wantedSeason
  // 每条只算一次（排序比较会反复问同一条）
  const ranks = new Map(deduped.map(item => [item, target === undefined ? 0 : fitRank(episodeFit(item, target))]))
  const rank = (item: SearchItem) => ranks.get(item) ?? 0
  for (const group of byName.values()) {
    group.items.sort((a, b) => rank(a) - rank(b) || Number(currentSeason(b)) - Number(currentSeason(a)) || Number(isBatchRelease(b)) - Number(isBatchRelease(a)) || seeders(b) - seeders(a) || (Date.parse(b.date ?? '') || 0) - (Date.parse(a.date ?? '') || 0))
  }
  // 组按「最贴近这一集的那条」排：有这一集单集的组在只有说不准合集的组前面，都没有的排最后
  const best = (group: FansubGroup) => rank(group.items[0]!)
  return [...byName.values()].sort((a, b) => best(a) - best(b) || Number(b.name === remembered) - Number(a.name === remembered) || b.items.length - a.items.length || a.name.localeCompare(b.name, 'zh'))
}

export interface ReleaseResultsProps {
  items: SearchItem[]
  outcomes: SourceOutcome[]
  sources: SourcesData
  engineDown: boolean
  pluginPending: boolean
  /** 插件来源搜索中断的原因；null 表示没有出错 */
  pluginError: string | null
  busy: boolean
  mediaId: number
  /** 目录作品的季数；说不准时缺省 */
  wantedSeason?: number
  /** 选定的集；null 表示不分集（剧场版） */
  target: EpisodeTarget | null
  onRetryPlugin: () => void
  onPlay: (item: SearchItem, button: HTMLButtonElement) => void
}

export function TorrentReleaseResults(props: ReleaseResultsProps) {
  const { items, outcomes, sources, engineDown, pluginPending, pluginError, busy, mediaId, wantedSeason, target, onPlay } = props
  const remembered = rememberedFansub(mediaId)
  // 照 animego 只排序、不隐藏：这一集的发布排在前面，别的集、别的季、特典照常列出
  const groups = groupByFansub(items, remembered, { ...(wantedSeason === undefined ? {} : { wantedSeason }), ...(target === null ? {} : { target }) })
  const [chosen, setChosen] = useState<string | null>(null)
  const first = groups[0]?.name ?? null
  // 用户没点过时，默认字幕组在第一次有结果时就定下来：插件结果还在流入、排序会变，
  // 面板不能在用户伸手点「播放」时换成别的字幕组
  useEffect(() => { if (chosen === null && first !== null) setChosen(first) }, [chosen, first])
  const active = groups.find(group => group.name === chosen) ?? groups[0] ?? null

  // 本机规则和插件 BT 来源都没有时才算「没配源」；插件给了结果就照常展示
  if (sources.sources.length === 0 && outcomes.length === 0 && !pluginPending && pluginError === null) return <div className="media-resource-empty">
    <p className="result result--warn">尚未配置资源源：启用本地来源插件（Nagare Source）或添加规则仓库。</p>
    <a className="btn btn--sm" href="/settings#sources">去设置</a>
  </div>

  const trouble = outcomes.filter(outcome => outcome.state === 'dead' || outcome.state === 'failed')
  const sourceNames = new Map(sources.sources.map(source => [source.id, source.name]))
  const play = (item: SearchItem, button: HTMLButtonElement, group: string) => {
    if (group !== UNGROUPED) rememberFansub(mediaId, group)
    onPlay(item, button)
  }
  const sourceLabel = (item: SearchItem) => sourceNames.get(item.source) ?? (item.source.startsWith('plugin:') ? `插件 · ${item.provider ?? item.source.slice('plugin:'.length)}` : item.source)
  const itemMeta = (item: SearchItem) => [item.resolution, item.size, typeof item.seeders === 'number' ? `做种 ${item.seeders}` : null, publishedLabel(item.date), sourceLabel(item)].filter(Boolean).join(' · ')
  const playButton = (item: SearchItem, group: string, label: string) => <button type="button" className="btn btn--sm btn--primary" disabled={busy || engineDown}
    title={busy ? '已有磁力任务，请先停止底部状态条中的任务' : undefined}
    onClick={event => play(item, event.currentTarget, group)}>{label}</button>
  const rowLabel = (item: SearchItem) => [
    releaseLabel(item, target),
    typeof item.season === 'number' ? `第 ${item.season} 季` : null,
    item.kind && item.kind !== 'main' && item.kind !== 'batch' ? item.kind.toUpperCase() : null,
    itemMeta(item),
  ].filter(Boolean).join(' · ')
  const hit = (item: SearchItem) => target !== null && matchesEpisode(item, target)
  const total = groups.reduce((n, group) => n + group.items.length, 0)
  const matched = target === null ? 0 : groups.reduce((n, group) => n + group.items.filter(hit).length, 0)
  const countLabel = [`${groups.length} 个分类`, `${total} 个版本`, target === null ? null : `第 ${target.episode} 集 ${matched} 个`].filter(Boolean).join(' · ')

  return <section className="media-resource-results" aria-label="按字幕组浏览磁力资源">
    <div className="media-play-selection"><h3>字幕组</h3><span className="result result--dim">{countLabel}{pluginPending ? ' · 插件来源仍在搜索…' : ''}</span></div>
    {/* 流入时每条都会改动计数：读屏只在搜完时念一次 */}
    <span className="visually-hidden" role="status">{pluginPending ? '插件来源仍在搜索' : countLabel}</span>
    {engineDown && <p className="result result--err" role="alert">磁力引擎不可用。<a className="link" href="/settings#torrent">查看设置</a></p>}
    {trouble.length > 0 && <p className="result result--warn" role="status">{sourceTrouble(trouble)}</p>}
    {pluginError !== null && <p className="result result--warn" role="alert">插件来源搜索中断：{pluginError} <button type="button" className="link" onClick={props.onRetryPlugin}>重新搜索插件来源</button></p>}
    {target !== null && total > 0 && matched === 0 && <p className="media-play-hint" role="status">{pluginPending ? `正在查找第 ${target.episode} 集的发布…` : `没有找到第 ${target.episode} 集的发布，下面列出全部 ${total} 个版本。`}</p>}
    {total === 0 ? <EmptyReleases pluginPending={pluginPending} /> : <div className="media-fansub-browser">
      <div className="media-fansub-row" role="group" aria-label="选择字幕组">
        {groups.map(group => <button type="button" key={group.name} aria-pressed={active?.name === group.name}
          className={active?.name === group.name ? 'media-fansub-chip media-fansub-chip--active' : 'media-fansub-chip'}
          aria-label={`字幕组 ${group.name}`} onClick={() => setChosen(group.name)}>
          <span className="media-fansub-avatar" aria-hidden="true">{group.name.slice(0, 2)}</span><strong>{group.name}</strong>
          <span>{group.items.length} 个版本{group.items.some(isBatchRelease) ? ' · 含合集' : ''}{group.name === remembered ? ' · 上次选择' : ''}</span>
        </button>)}
      </div>
      {active && <div className="media-fansub-detail" role="group" aria-label={`${active.name} 的资源`}>
        <div className="media-fansub-heading"><h3>{active.name}</h3><span>{[...new Set(active.items.map(item => item.resolution).filter(Boolean))].join(' / ')}</span></div>
        <ul className="media-resource-list">
          {active.items.slice(0, MAX_RESOURCE_RESULTS).map(item => <li key={releaseKey(item)} className={hit(item) ? 'media-resource-hit' : undefined}>
            <div>{hit(item) && <span className="visually-hidden">第 {target?.episode} 集：</span>}<strong>{item.title}</strong><span>{rowLabel(item)}</span></div>
            {playButton(item, active.name, isBatchRelease(item) || item.episode == null ? '选择文件' : '播放')}
          </li>)}
        </ul>
        {active.items.length > MAX_RESOURCE_RESULTS && <p className="media-play-hint">仅显示前 {MAX_RESOURCE_RESULTS} 个版本，请细化作品名称继续搜索。</p>}
      </div>}
    </div>}
  </section>
}

function EmptyReleases({ pluginPending }: { pluginPending: boolean }) {
  return <p className="media-play-hint" role="status">{pluginPending ? '本机规则没有结果，插件来源仍在搜索…' : '没有找到发布版本。可以修改作品名称后重试。'}</p>
}

/** 一行发布的集号说明：单集写集号（连续编号的注明对应哪一集），合集写覆盖区间。 */
function releaseLabel(item: SearchItem, target: EpisodeTarget | null): string {
  if (isBatchRelease(item)) {
    const range = item.episodeRange
    return range === undefined ? '整季合集 · 播放前选文件' : `合集 第 ${range.low}–${range.high} 集`
  }
  if (typeof item.episode !== 'number') return '播放前选择文件'
  if (target?.offset !== undefined && item.episode === target.episode + target.offset && (item.kind ?? 'main') === 'main') {
    return `第 ${item.episode} 集（连续编号，即第 ${target.episode} 集）`
  }
  return `第 ${item.episode} 集`
}

/** 发布日期只显示到月；老发布（两年以上）标出来，提醒用户可能已经没人做种。 */
export function publishedLabel(date: string | null | undefined): string | null {
  const ms = date == null ? null : parsePublished(date)
  if (ms === null) return null
  const d = new Date(ms)
  const label = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
  return Date.now() - ms > 2 * 365 * 24 * 3600 * 1000 ? `${label} 发布（较旧，可能无人做种）` : `${label} 发布`
}

function sourceTrouble(outcomes: SourceOutcome[]): string {
  const dead = outcomes.filter(outcome => outcome.state === 'dead').length
  const failed = outcomes.filter(outcome => outcome.state === 'failed').length
  return [dead ? `${dead} 个源规则异常` : '', failed ? `${failed} 个源连接失败` : ''].filter(Boolean).join('，')
}
