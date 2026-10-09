import { useState } from 'react'
import type { SearchItem, SourceOutcome, SourcesData } from '../../lib/endpoints'
import { parsePublished } from '../../lib/format'
import { episodeFit, fitRank, isBatchRelease, releasesForEpisode } from './releaseEpisodes'
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

/** 同一个种子会从多个来源（Anime Garden / 動漫花園 / Mikan…）各来一次，按 infohash 折叠。 */
export function releaseKey(item: SearchItem): string {
  const hash = item.infohash?.toLowerCase() || /btih:([0-9a-z]{32,40})/i.exec(item.magnet)?.[1]?.toLowerCase()
  return hash ?? (item.torrentUrl ?? item.magnet ?? item.title).toLowerCase()
}

export interface FansubGroupingOptions {
  /** 目录作品的季数；说不准时缺省，组内就不按季数排 */
  wantedSeason?: number
  /** 选定的集：给了就按「这一集的单集 → 含这一集的合集 → 说不准的合集」排组内顺序 */
  target?: EpisodeTarget
}

export function groupByFansub(items: SearchItem[], remembered: string | null, options: FansubGroupingOptions = {}): FansubGroup[] {
  const { wantedSeason, target } = options
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
      // 同一种子以带做种数的那条为准（排序靠它）；插件指明的文件下标不能在折叠时丢掉
      deduped[deduped.indexOf(existing)] = { ...item, group: existing.group ?? item.group, fileIndex: item.fileIndex ?? existing.fileIndex }
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
  const currentSeason = (item: SearchItem) => wantedSeason !== undefined && (item.season ?? 1) === wantedSeason
  // 每条只算一次（排序比较会反复问同一条）
  const ranks = new Map(deduped.map(item => [item, target === undefined ? 0 : fitRank(episodeFit(item, target))]))
  const rank = (item: SearchItem) => ranks.get(item) ?? 0
  for (const group of byName.values()) {
    group.items.sort((a, b) => rank(a) - rank(b) || Number(currentSeason(b)) - Number(currentSeason(a)) || Number(isBatchRelease(b)) - Number(isBatchRelease(a)) || seeders(b) - seeders(a) || (Date.parse(b.date ?? '') || 0) - (Date.parse(a.date ?? '') || 0))
  }
  return [...byName.values()].sort((a, b) => Number(b.name === remembered) - Number(a.name === remembered) || b.items.length - a.items.length || a.name.localeCompare(b.name, 'zh'))
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
  /** 选定的集；null 表示不按集筛（剧场版） */
  target: EpisodeTarget | null
  showAll: boolean
  onShowAll: (showAll: boolean) => void
  onRetryPlugin: () => void
  onPlay: (item: SearchItem, button: HTMLButtonElement) => void
}

export function TorrentReleaseResults(props: ReleaseResultsProps) {
  const { items, outcomes, sources, engineDown, pluginPending, pluginError, busy, mediaId, wantedSeason, target, showAll, onShowAll, onPlay } = props
  const remembered = rememberedFansub(mediaId)
  const filtering = target !== null && !showAll
  const visible = filtering ? releasesForEpisode(items, target) : items
  const groups = groupByFansub(visible, remembered, { ...(wantedSeason === undefined ? {} : { wantedSeason }), ...(filtering ? { target } : {}) })
  const [chosen, setChosen] = useState<string | null>(null)
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
  const hit = (item: SearchItem) => target !== null && episodeFit(item, target) !== 'other'
  const total = new Set(items.map(releaseKey)).size
  const countLabel = `${groups.length} 个分类 · ${groups.reduce((n, group) => n + group.items.length, 0)} 个版本`
  const heading = filtering ? `第 ${target.episode} 集的字幕组` : '字幕组'
  // 文案随状态变，就不再标 aria-pressed（读屏会念成「只看第 2 集，已按下」）
  const toggle = target === null ? null : <button type="button" className="btn btn--sm" onClick={() => onShowAll(!showAll)}>
    {showAll ? `只看第 ${target.episode} 集` : `显示全部发布（${total} 个）`}
  </button>

  return <section className="media-resource-results" aria-label="按字幕组浏览磁力资源">
    <div className="media-play-selection"><h3>{heading}</h3><span className="result result--dim" role="status">{countLabel}{pluginPending ? ' · 插件来源仍在搜索…' : ''}</span>{toggle}</div>
    {engineDown && <p className="result result--err" role="alert">磁力引擎不可用。<a className="link" href="/settings#torrent">查看设置</a></p>}
    {trouble.length > 0 && <p className="result result--warn" role="status">{sourceTrouble(trouble)}</p>}
    {pluginError !== null && <p className="result result--warn" role="alert">插件来源搜索中断：{pluginError} <button type="button" className="link" onClick={props.onRetryPlugin}>重新搜索插件来源</button></p>}
    {visible.length === 0 ? <EmptyReleases {...props} total={total} filtering={filtering} /> : <div className="media-fansub-browser">
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
          {active.items.slice(0, MAX_RESOURCE_RESULTS).map(item => <li key={releaseKey(item)} className={!filtering && hit(item) ? 'media-resource-hit' : undefined}>
            <div><strong>{item.title}</strong><span>{rowLabel(item)}</span></div>
            {playButton(item, active.name, isBatchRelease(item) || item.episode == null ? '选择文件' : '播放')}
          </li>)}
        </ul>
        {active.items.length > MAX_RESOURCE_RESULTS && <p className="media-play-hint">仅显示前 {MAX_RESOURCE_RESULTS} 个版本，请细化作品名称继续搜索。</p>}
      </div>}
    </div>}
  </section>
}

function EmptyReleases({ pluginPending, target, total, filtering }: ReleaseResultsProps & { total: number; filtering: boolean }) {
  if (filtering && target !== null) {
    // 入口就是上方的「显示全部发布」：这里再放一个按钮，点完它自己消失，焦点会掉到页面上
    const others = total > 0 ? `其余 ${total} 个版本可点上方「显示全部发布」查看。` : ''
    return <p className="media-play-hint" role="status">{pluginPending ? `正在查找第 ${target.episode} 集的发布…` : `没有找到第 ${target.episode} 集的发布。${others}`}</p>
  }
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
