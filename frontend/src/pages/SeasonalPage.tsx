import { useMemo } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { CatalogNotice, PosterSkeletons } from '../components/catalog/CatalogStates'
import { FreshnessNote } from '../components/catalog/FreshnessNote'
import { RouteLink } from '../components/catalog/RouteLink'
import { SEASONS, SEASON_LABEL, adjacentSeason, currentSeason } from '../components/catalog/season'
import { DiscoverCard } from '../components/media/DiscoverCard'
import { GENRES, genreQuery } from '../components/media/DiscoverGenres'
import type { MediaSummary } from '../components/media/types'
import { Tab, Tabs } from '../components/ui'
import { Icon } from '../components/ui/Icon'
import { useCatalogResource } from '../hooks/useCatalogResource'
import type { SeasonName } from '../lib/endpoints'
import { fetchSeasonal } from '../lib/media'
import '../components/catalog/catalog.css'
import '../components/media/media.css'
import './seasonal.css'

export { SEASONS, adjacentSeason, currentSeason } from '../components/catalog/season'

const FORMATS = ['TV', 'TV_SHORT', 'MOVIE', 'OVA', 'ONA', 'SPECIAL'] as const
const FORMAT_LABEL: Record<string, string> = { TV: 'TV', TV_SHORT: '短篇', MOVIE: '剧场版', OVA: 'OVA', ONA: 'ONA', SPECIAL: '特别篇' }
const STATUSES = ['RELEASING', 'FINISHED', 'NOT_YET_RELEASED'] as const
const STATUS_LABEL: Record<string, string> = { RELEASING: '连载中', FINISHED: '已完结', NOT_YET_RELEASED: '未播出' }
const SORTS = ['score', 'title', 'format'] as const
type SortKey = (typeof SORTS)[number]
const SORT_LABEL: Record<SortKey, string> = { score: '按评分', title: '按标题', format: '按格式' }
const MIN_YEAR = 1990

export interface SeasonalSearch { season?: SeasonName; year?: number; genre?: string; format?: string; status?: string; sort?: SortKey; [key: string]: unknown }

/** 客户端筛选与排序：一季只有一页（≤200 条），没必要为筛选再请求。 */
export function applySeasonalFilters(items: MediaSummary[], search: SeasonalSearch): MediaSummary[] {
  let list = items
  const genre = search.genre ? genreQuery(search.genre) : ''
  if (genre) list = list.filter(item => item.genres.includes(genre))
  if (search.format) list = list.filter(item => item.format === search.format)
  if (search.status) list = list.filter(item => item.status === search.status)
  const sorted = [...list]
  switch (search.sort ?? 'score') {
    case 'title': sorted.sort((a, b) => a.title.localeCompare(b.title, 'zh')); break
    case 'format': sorted.sort((a, b) => FORMATS.indexOf(a.format as typeof FORMATS[number]) - FORMATS.indexOf(b.format as typeof FORMATS[number]) || (b.score ?? 0) - (a.score ?? 0)); break
    default: sorted.sort((a, b) => (b.score ?? 0) - (a.score ?? 0))
  }
  return sorted
}

/** 去掉未定义的键，地址栏只留真正设置了的参数；默认排序也不写。 */
function prune(search: SeasonalSearch): SeasonalSearch {
  const out: SeasonalSearch = {}
  if (search.season) out.season = search.season
  if (search.year) out.year = search.year
  if (search.genre) out.genre = search.genre
  if (search.format) out.format = search.format
  if (search.status) out.status = search.status
  if (search.sort && search.sort !== 'score') out.sort = search.sort
  return out
}

/** 季度浏览（`/seasonal`）：年份 + 季度、类型 / 格式 / 状态筛选、排序都在地址栏，可分享可回退 */
export function SeasonalPage() {
  const search = useSearch({ strict: false }) as SeasonalSearch
  const navigate = useNavigate()
  const fallback = currentSeason()
  const season = search.season ?? fallback.season
  const year = search.year ?? fallback.year
  const resource = useCatalogResource(`${season}-${year}`, signal => fetchSeasonal(season, year, signal), { fallbackError: '季度目录读取失败' })
  const items = resource.data?.items
  const filtered = useMemo(() => (items ? applySeasonalFilters(items, search) : []), [items, search])
  const years = useMemo(() => Array.from({ length: fallback.year + 1 - MIN_YEAR + 1 }, (_, i) => fallback.year + 1 - i), [fallback.year])
  const go = (next: Partial<SeasonalSearch>) => void navigate({ to: '/seasonal', search: prune({ ...search, season, year, ...next }) })
  const prev = adjacentSeason(season, year, -1)
  const next = adjacentSeason(season, year, 1)
  const hasFilters = Boolean(search.genre || search.format || search.status)
  const clearFilters = () => go({ genre: undefined, format: undefined, status: undefined })

  return <main className="catalog-page">
    <header className="catalog-head">
      <h1 className="catalog-title">{year} 年{SEASON_LABEL[season]}季新番</h1>
      <nav className="seasonal-nav" aria-label="季度导航">
        <RouteLink to="/seasonal" search={prune({ ...search, ...prev })} className="seasonal-step"><Icon name="left" size={16} /><span className="visually-hidden">上一季：</span>{prev.year} {SEASON_LABEL[prev.season]}</RouteLink>
        <div className="catalog-segments">
          <Tabs value={season} onChange={value => go({ season: value as SeasonName })} label="季度">
            {SEASONS.map(value => <Tab key={value} value={value}>{SEASON_LABEL[value]}季</Tab>)}
          </Tabs>
        </div>
        <label><span className="visually-hidden">年份</span>
          <select className="seasonal-select" value={year} onChange={event => go({ year: Number(event.target.value) })}>
            {years.map(value => <option key={value} value={value}>{value}</option>)}
          </select>
        </label>
        <RouteLink to="/seasonal" search={prune({ ...search, ...next })} className="seasonal-step"><span className="visually-hidden">下一季：</span>{next.year} {SEASON_LABEL[next.season]}<Icon name="right" size={16} /></RouteLink>
      </nav>
    </header>

    <section className="seasonal-filters" aria-label="筛选">
      <div className="seasonal-chip-row seasonal-chip-row--scroll" role="group" aria-label="类型">
        {GENRES.map(label => <button type="button" key={label} className="catalog-chip" aria-pressed={(search.genre ?? '全部') === label}
          onClick={() => go({ genre: label === '全部' ? undefined : label })}>{label}</button>)}
      </div>
      <div className="seasonal-chip-row" role="group" aria-label="格式与状态">
        {FORMATS.map(value => <button type="button" key={value} className="catalog-chip" aria-pressed={search.format === value}
          onClick={() => go({ format: search.format === value ? undefined : value })}>{FORMAT_LABEL[value]}</button>)}
        <span className="seasonal-chip-gap" aria-hidden="true" />
        {STATUSES.map(value => <button type="button" key={value} className="catalog-chip" aria-pressed={search.status === value}
          onClick={() => go({ status: search.status === value ? undefined : value })}>{STATUS_LABEL[value]}</button>)}
      </div>
      <div className="seasonal-sort-row">
        <span className="seasonal-count" role="status">{items ? `${filtered.length} 部${hasFilters ? `（共 ${items.length} 部）` : ''}` : ''}</span>
        <FreshnessNote freshness={resource.data?.freshness} refreshing={resource.refreshing} />
        <label className="seasonal-sort">排序
          <select className="seasonal-select" value={search.sort ?? 'score'} onChange={event => go({ sort: event.target.value as SortKey })}>
            {SORTS.map(value => <option key={value} value={value}>{SORT_LABEL[value]}</option>)}
          </select>
        </label>
        {hasFilters && <button type="button" className="link seasonal-clear" onClick={clearFilters}>清除筛选</button>}
      </div>
    </section>

    {resource.loading && <PosterSkeletons label={`正在读取 ${year} 年${SEASON_LABEL[season]}季`} />}
    {resource.error && <CatalogNotice tone="error" title="季度目录读取失败" actions={<button type="button" className="btn" onClick={resource.retry}>重试</button>}>{resource.error}</CatalogNotice>}
    {items && (filtered.length > 0
      ? <ul className="catalog-grid">{filtered.map(media => <DiscoverCard key={media.id} media={media} />)}</ul>
      : items.length === 0
        ? <CatalogNotice title="这一季暂时没有作品">目录里还没有这一季的条目，换一季看看。</CatalogNotice>
        : <CatalogNotice title="没有符合筛选的作品" actions={<button type="button" className="btn" onClick={clearFilters}>清除筛选</button>}>放宽类型、格式或状态的条件再看看。</CatalogNotice>)}
  </main>
}
