import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { DiscoverCard } from '../components/media/DiscoverCard'
import { GENRES, genreQuery } from '../components/media/DiscoverGenres'
import { Result, Tab, Tabs } from '../components/ui'
import { Icon } from '../components/ui/Icon'
import { fetchSeasonal } from '../lib/media'
import type { SeasonName } from '../lib/media'
import { errorText } from '../lib/format'
import type { MediaSummary } from '../components/media/types'
import '../components/library/library.css'
import '../components/media/media.css'
import '../components/media/discover.css'
import './seasonal.css'

/** 季度顺序与 animego 一致：冬（1–3 月）→ 春 → 夏 → 秋。 */
export const SEASONS: readonly SeasonName[] = ['WINTER', 'SPRING', 'SUMMER', 'FALL']
const SEASON_LABEL: Record<SeasonName, string> = { WINTER: '冬', SPRING: '春', SUMMER: '夏', FALL: '秋' }
const FORMATS = ['TV', 'TV_SHORT', 'MOVIE', 'OVA', 'ONA', 'SPECIAL'] as const
const FORMAT_LABEL: Record<string, string> = { TV: 'TV', TV_SHORT: '短篇', MOVIE: '剧场版', OVA: 'OVA', ONA: 'ONA', SPECIAL: '特别篇' }
const STATUSES = ['RELEASING', 'FINISHED', 'NOT_YET_RELEASED'] as const
const STATUS_LABEL: Record<string, string> = { RELEASING: '连载中', FINISHED: '已完结', NOT_YET_RELEASED: '未播出' }
const SORTS = ['score', 'title', 'format'] as const
type SortKey = (typeof SORTS)[number]
const SORT_LABEL: Record<SortKey, string> = { score: '按评分', title: '按标题', format: '按格式' }
const MIN_YEAR = 1990

export interface SeasonalSearch { season?: SeasonName; year?: number; genre?: string; format?: string; status?: string; sort?: SortKey; [key: string]: unknown }

/** 当前所处的季度（本地时间），用作默认值与年份下拉的上限。 */
export function currentSeason(now = new Date()): { season: SeasonName; year: number } {
  return { season: SEASONS[Math.floor(now.getMonth() / 3)]!, year: now.getFullYear() }
}

/** 相邻季度，跨年回绕：冬 <年> 的上一季是秋 <年-1>。 */
export function adjacentSeason(season: SeasonName, year: number, dir: -1 | 1): { season: SeasonName; year: number } {
  const next = SEASONS.indexOf(season) + dir
  if (next < 0) return { season: 'FALL', year: year - 1 }
  if (next >= SEASONS.length) return { season: 'WINTER', year: year + 1 }
  return { season: SEASONS[next]!, year }
}

/** 客户端筛选与排序：animego 一季只有一页（≤200 条），没必要为筛选再请求。 */
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

export function SeasonalPage() {
  const search = useSearch({ strict: false }) as SeasonalSearch
  const navigate = useNavigate()
  const fallback = currentSeason()
  const season = search.season ?? fallback.season
  const year = search.year ?? fallback.year
  const [items, setItems] = useState<MediaSummary[]>()
  const [error, setError] = useState<string>()
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const abort = new AbortController()
    setItems(undefined)
    setError(undefined)
    void fetchSeasonal(season, year, abort.signal)
      .then(next => { if (!abort.signal.aborted) setItems(next) })
      .catch(err => { if (!abort.signal.aborted) setError(errorText(err, '季度目录加载失败')) })
    return () => abort.abort()
  }, [season, year, attempt])

  const filtered = useMemo(() => (items ? applySeasonalFilters(items, search) : []), [items, search])
  const years = useMemo(() => Array.from({ length: fallback.year + 1 - MIN_YEAR + 1 }, (_, i) => fallback.year + 1 - i), [fallback.year])
  const go = (next: Partial<SeasonalSearch>) => void navigate({ to: '/seasonal', search: prune({ ...search, season, year, ...next }) })
  const prev = adjacentSeason(season, year, -1)
  const next = adjacentSeason(season, year, 1)
  const hasFilters = Boolean(search.genre || search.format || search.status)

  return <main className="lib-shell seasonal-shell">
    <header className="page-head seasonal-head">
      <h1 className="page-title">{year} 年 {SEASON_LABEL[season]}季新番</h1>
      <nav className="seasonal-nav" aria-label="季度导航">
        <Link to="/seasonal" search={prune({ ...search, ...prev })} className="btn btn--sm" aria-label={`上一季：${prev.year} 年${SEASON_LABEL[prev.season]}季`}><Icon name="left" size={16} />{prev.year} {SEASON_LABEL[prev.season]}</Link>
        <Tabs value={season} onChange={value => go({ season: value as SeasonName })} label="季度">
          {SEASONS.map(value => <Tab key={value} value={value}>{SEASON_LABEL[value]}季</Tab>)}
        </Tabs>
        <label className="seasonal-year"><span className="visually-hidden">年份</span>
          <select className="input" value={year} onChange={event => go({ year: Number(event.target.value) })} aria-label="年份">
            {years.map(value => <option key={value} value={value}>{value}</option>)}
          </select>
        </label>
        <Link to="/seasonal" search={prune({ ...search, ...next })} className="btn btn--sm" aria-label={`下一季：${next.year} 年${SEASON_LABEL[next.season]}季`}>{next.year} {SEASON_LABEL[next.season]}<Icon name="right" size={16} /></Link>
      </nav>
    </header>

    <section className="seasonal-filters" aria-label="筛选">
      <div className="seasonal-chip-row" role="group" aria-label="类型">
        {GENRES.map(label => <button type="button" key={label} className={(search.genre ?? '全部') === label ? 'seasonal-chip seasonal-chip--active' : 'seasonal-chip'} aria-pressed={(search.genre ?? '全部') === label}
          onClick={() => go({ genre: label === '全部' ? undefined : label })}>{label}</button>)}
      </div>
      <div className="seasonal-chip-row" role="group" aria-label="格式与状态">
        {FORMATS.map(value => <button type="button" key={value} className={search.format === value ? 'seasonal-chip seasonal-chip--active' : 'seasonal-chip'} aria-pressed={search.format === value}
          onClick={() => go({ format: search.format === value ? undefined : value })}>{FORMAT_LABEL[value]}</button>)}
        <span className="seasonal-chip-gap" aria-hidden="true" />
        {STATUSES.map(value => <button type="button" key={value} className={search.status === value ? 'seasonal-chip seasonal-chip--active' : 'seasonal-chip'} aria-pressed={search.status === value}
          onClick={() => go({ status: search.status === value ? undefined : value })}>{STATUS_LABEL[value]}</button>)}
      </div>
      <div className="seasonal-sort-row">
        <span className="result result--dim" role="status">{items ? `${filtered.length} 部${hasFilters ? `（共 ${items.length} 部）` : ''}` : ''}</span>
        <label>排序 <select className="input" value={search.sort ?? 'score'} onChange={event => go({ sort: event.target.value as SortKey })} aria-label="排序">
          {SORTS.map(value => <option key={value} value={value}>{SORT_LABEL[value]}</option>)}
        </select></label>
        {hasFilters && <button type="button" className="link" onClick={() => go({ genre: undefined, format: undefined, status: undefined })}>清除筛选</button>}
      </div>
    </section>

    {error && <p className="result result--err" role="alert">{error} <button className="btn" onClick={() => setAttempt(value => value + 1)}>重新加载</button></p>}
    {!items && !error && <Result live>正在读取 {year} 年{SEASON_LABEL[season]}季…</Result>}
    {items && (filtered.length === 0
      ? <Result>{items.length === 0 ? '这一季暂时没有作品。' : '没有符合筛选的作品。'}</Result>
      : <ul className="poster-grid">{filtered.map(media => <DiscoverCard key={media.id} media={media} />)}</ul>)}
  </main>
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
