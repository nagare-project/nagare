import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { CatalogNotice } from '../components/catalog/CatalogStates'
import { FreshnessNote } from '../components/catalog/FreshnessNote'
import { SEASONAL_SORTS, SEASONS, SEASON_LABEL, currentSeason } from '../components/catalog/season'
import type { SeasonalSortKey } from '../components/catalog/season'
import {
  BrowseHueDots,
  BrowseResults,
  buildHueOptions,
  MenuField,
  SOURCE_LABELS,
  STATUS_LABELS,
  type BrowseViewMode,
} from '../components/browse/BrowseCards'
import {
  ChartIcon,
  CheckIcon,
  CloseIcon,
  GridIcon,
  ListIcon,
  SearchIcon,
  SlidersIcon,
  SortIcon,
  TagsIcon,
} from '../components/browse/icons'
import controls from '../components/browse/controls.module.css'
import frame from '../components/browse/frame.module.css'
import tags from '../components/browse/tags.module.css'
import { GENRES, genreQuery } from '../components/media/genres'
import { FORMAT_LABELS } from '../components/media/media-format'
import { useCatalogResource } from '../hooks/useCatalogResource'
import { fetchSeasonalData, type SeasonName } from '../lib/endpoints'
import { HUE_FAMILIES, hueFamilyOf, type HueFamilyKey } from '../lib/home/hueFamilies'
import { clockParts } from '../lib/home/time'
import { oklchCss } from '../lib/home/tone'

export { SEASONS, adjacentSeason, currentSeason } from '../components/catalog/season'

const FORMATS = ['TV', 'TV_SHORT', 'MOVIE', 'OVA', 'ONA', 'SPECIAL'] as const
const STATUSES = ['RELEASING', 'FINISHED', 'NOT_YET_RELEASED'] as const
const SOURCES = ['ORIGINAL', 'MANGA', 'LIGHT_NOVEL', 'VIDEO_GAME', 'OTHER'] as const
const SORTS = SEASONAL_SORTS
type SortKey = SeasonalSortKey
const SORT_LABEL: Record<SortKey, string> = { score: '按评分', title: '按标题', format: '按格式' }
const WEEKDAY_ORDER = [1, 2, 3, 4, 5, 6, 0] as const
const WEEKDAY_SHORT = ['日', '一', '二', '三', '四', '五', '六'] as const
const WEEKDAY_LONG = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'] as const
const MIN_YEAR = 1990

type OpenMenu = 'genre' | 'year' | 'season' | 'format' | 'more' | 'sort' | null

export interface SeasonalSearch {
  season?: SeasonName
  year?: number
  genre?: string
  format?: string
  status?: string
  source?: string
  sort?: SortKey
  view?: BrowseViewMode | 'cover'
  hue?: HueFamilyKey
  q?: string
  wd?: number
  [key: string]: unknown
}

export interface FilterableSeasonalItem {
  title: string
  titleNative?: string
  titleRomaji?: string
  titleEnglish?: string
  genres: string[]
  format?: string
  status?: string
  source?: string
  score?: number
  hue?: number | null
  posterTones?: { hue: number } | null
  nextAiring?: { episode: number; at: number }
}

/** 客户端筛选与排序：一季只有一页（≤200 条），没必要为筛选再请求。 */
export function applySeasonalFilters<T extends FilterableSeasonalItem>(items: T[], search: SeasonalSearch): T[] {
  let list = items
  const genre = search.genre ? genreQuery(search.genre) : ''
  if (genre) list = list.filter((item) => item.genres.includes(genre))
  if (search.format) list = list.filter((item) => item.format === search.format)
  if (search.status) list = list.filter((item) => item.status === search.status)
  if (search.source) {
    list =
      search.source === 'OTHER'
        ? list.filter((item) => item.source && !['ORIGINAL', 'MANGA', 'LIGHT_NOVEL', 'VIDEO_GAME'].includes(item.source))
        : list.filter((item) => item.source === search.source)
  }
  if (search.hue) {
    list = list.filter((item) => hueFamilyOf(item.hue ?? item.posterTones?.hue ?? null) === search.hue)
  }
  if (typeof search.wd === 'number' && search.wd >= 0 && search.wd <= 6) {
    list = list.filter((item) => item.nextAiring && clockParts(item.nextAiring.at * 1000).weekday === search.wd)
  }
  const q = search.q?.trim().toLowerCase()
  if (q) {
    list = list.filter((item) =>
      [item.title, item.titleNative, item.titleRomaji, item.titleEnglish].some((s) => s?.toLowerCase().includes(q)),
    )
  }
  const sorted = [...list]
  switch (search.sort ?? 'score') {
    case 'title':
      sorted.sort((a, b) => a.title.localeCompare(b.title, 'zh'))
      break
    case 'format':
      sorted.sort(
        (a, b) =>
          FORMATS.indexOf(a.format as (typeof FORMATS)[number]) -
            FORMATS.indexOf(b.format as (typeof FORMATS)[number]) || (b.score ?? 0) - (a.score ?? 0),
      )
      break
    default:
      sorted.sort((a, b) => (b.score ?? 0) - (a.score ?? 0))
  }
  return sorted
}

function prune(search: SeasonalSearch): SeasonalSearch {
  const out: SeasonalSearch = {}
  if (search.season) out.season = search.season
  if (search.year) out.year = search.year
  if (search.genre) out.genre = search.genre
  if (search.format) out.format = search.format
  if (search.status) out.status = search.status
  if (search.source) out.source = search.source
  if (search.sort && search.sort !== 'score') out.sort = search.sort
  if (search.view && search.view !== 'grid') out.view = search.view === 'cover' ? 'grid' : search.view
  if (search.hue) out.hue = search.hue
  if (search.q?.trim()) out.q = search.q.trim()
  if (typeof search.wd === 'number' && search.wd >= 0 && search.wd <= 6) out.wd = search.wd
  return out
}

function normalizeView(v?: string): BrowseViewMode {
  if (v === 'chart' || v === 'table') return v
  return 'grid'
}

/**
 * 季度浏览（`/seasonal`，1:1 对齐网站 SeasonalBrowse）：
 * HeadRow（标题 + 总数 + 7 日放送小柱状图）→ 5 列 FilterBar + 更多筛选 →
 * TagRow（已选标签 + 展开式色调圆点 + 排序菜单 + 图标视图切换）→ 封面墙/图表卡/表格三视图。
 */
export function SeasonalPage() {
  const search = useSearch({ from: '/seasonal' }) as SeasonalSearch
  const navigate = useNavigate()
  const fallback = currentSeason()
  const season = search.season ?? fallback.season
  const year = search.year ?? fallback.year
  const view = normalizeView(search.view)
  const sort: SortKey = search.sort ?? 'score'

  const [menu, setMenu] = useState<OpenMenu>(null)
  const [mobileFiltersOpen, setMobileFiltersOpen] = useState(false)
  const sortTriggerRef = useRef<HTMLButtonElement | null>(null)

  const resource = useCatalogResource(
    `${season}-${year}`,
    (signal) => fetchSeasonalData(season, year, signal),
    { fallbackError: '季度目录读取失败' },
  )
  const items = resource.data?.items
  const filtered = useMemo(() => (items ? applySeasonalFilters(items, search) : []), [items, search])
  const years = useMemo(
    () => Array.from({ length: fallback.year + 1 - MIN_YEAR + 1 }, (_, i) => fallback.year + 1 - i),
    [fallback.year],
  )
  const hueOptions = useMemo(() => buildHueOptions(items ?? [], hueFamilyOf), [items])

  const weekdayBars = useMemo(() => {
    const counts = new Map<number, number>()
    for (const item of items ?? []) {
      if (!item.nextAiring) continue
      const wd = clockParts(item.nextAiring.at * 1000).weekday
      counts.set(wd, (counts.get(wd) ?? 0) + 1)
    }
    const max = Math.max(0, ...counts.values())
    if (max === 0) return []
    const todayWd = clockParts(Date.now()).weekday
    return WEEKDAY_ORDER.map((wd) => {
      const count = counts.get(wd) ?? 0
      return {
        wd,
        count,
        today: wd === todayWd,
        heightPx: count === 0 ? 4 : Math.max(6, Math.round((28 * count) / max)),
      }
    })
  }, [items])

  // 点外面或按 Esc 关闭浮层菜单
  useEffect(() => {
    if (!menu) return
    const onDown = (e: PointerEvent) => {
      const el = e.target as Element | null
      if (el?.closest?.('[data-menu-field],[data-menu]')) return
      setMenu(null)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenu(null)
    }
    document.addEventListener('pointerdown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [menu])

  const heading = `${year} 年${SEASON_LABEL[season]}季新番`
  useEffect(() => {
    const prev = document.title
    document.title = `${heading} · nagare`
    return () => { document.title = prev }
  }, [heading])

  const go = (next: Partial<SeasonalSearch>) =>
    void navigate({ to: '/seasonal', search: prune({ ...search, season, year, ...next }) })

  const activeFiltersCount = [
    search.genre,
    search.format,
    search.status,
    search.source,
    search.hue,
    search.q?.trim(),
    typeof search.wd === 'number' ? String(search.wd) : undefined,
  ].filter(Boolean).length
  const hasFilters = activeFiltersCount > 0
  const clearFilters = () =>
    go({ genre: undefined, format: undefined, status: undefined, source: undefined, hue: undefined, q: undefined, wd: undefined })

  const selectedHueObj = search.hue ? HUE_FAMILIES.find((f) => f.key === search.hue) : undefined
  const pageStyle: CSSProperties | undefined = selectedHueObj
    ? { backgroundColor: oklchCss(13, 0.014, selectedHueObj.hue) }
    : undefined

  const moreSet = Boolean(search.status || search.source)
  const genreOptions = GENRES.filter((g) => g !== '全部')

  return (
    <main className={frame.page} style={pageStyle}>
      <header className={frame.head}>
        <div className={frame.h1row}>
          <h1 className={frame.h1}>{heading}</h1>
          {items && (
            <span className={frame.count} role="status">
              共 {filtered.length} 部
            </span>
          )}
          <FreshnessNote freshness={resource.data?.freshness} refreshing={resource.refreshing} />
        </div>
        {weekdayBars.length > 0 && (
          <div className={frame.bars} role="group" aria-label="本周放送分布">
            <span className={frame.barsLabel} aria-hidden="true">本周放送</span>
            <div className={frame.barRow}>
              {weekdayBars.map((b) => {
                const on = search.wd === b.wd
                return (
                  <button
                    key={b.wd}
                    type="button"
                    className={frame.bar}
                    aria-pressed={on}
                    disabled={b.count === 0}
                    data-today={b.today ? 'true' : undefined}
                    onClick={() => go({ wd: on ? undefined : b.wd })}
                  >
                    <em className={frame.barCount}>
                      {b.count}
                      <span className="visually-hidden"> 部</span>
                    </em>
                    <span className={frame.barTrack} aria-hidden="true">
                      <i className={frame.barFill} style={{ height: b.heightPx }} />
                    </span>
                    <b className={frame.barLabel}>{WEEKDAY_SHORT[b.wd]}</b>
                  </button>
                )
              })}
            </div>
          </div>
        )}
      </header>

      {/* 桌面 5 列筛选栏（1:1 对齐网站 FilterBar） */}
      <section className={controls.filters} aria-label="筛选">
        <div className={controls.field}>
          <label className={controls.label} htmlFor="seasonal-search-input">搜索</label>
          <div className={`${controls.control} ${controls.search}`}>
            <SearchIcon />
            <input
              id="seasonal-search-input"
              type="search"
              autoComplete="off"
              placeholder="搜本季作品…"
              value={search.q ?? ''}
              onChange={(e) => go({ q: e.currentTarget.value || undefined })}
            />
          </div>
        </div>

        <MenuField
          label="类型"
          value={search.genre || '任意'}
          isSet={Boolean(search.genre)}
          open={menu === 'genre'}
          onToggle={() => setMenu(menu === 'genre' ? null : 'genre')}
          onClose={() => setMenu(null)}
          popup="listbox"
        >
          {({ labelId }) =>
            menu === 'genre' ? (
              <div className={`${controls.menu} ${controls.menuWide}`} role="listbox" aria-labelledby={labelId}>
                <button
                  type="button"
                  role="option"
                  aria-selected={!search.genre}
                  className={controls.opt}
                  onClick={() => { go({ genre: undefined }); setMenu(null) }}
                >
                  任意
                  <CheckIcon />
                </button>
                {genreOptions.map((g) => (
                  <button
                    key={g}
                    type="button"
                    role="option"
                    aria-selected={search.genre === g}
                    className={controls.opt}
                    onClick={() => { go({ genre: search.genre === g ? undefined : g }); setMenu(null) }}
                  >
                    {g}
                    <CheckIcon />
                  </button>
                ))}
              </div>
            ) : null
          }
        </MenuField>

        <MenuField
          label="年份"
          value={`${year} 年`}
          isSet
          open={menu === 'year'}
          onToggle={() => setMenu(menu === 'year' ? null : 'year')}
          onClose={() => setMenu(null)}
          popup="listbox"
        >
          {({ labelId }) =>
            menu === 'year' ? (
              <div className={controls.menu} role="listbox" aria-labelledby={labelId}>
                {years.map((y) => (
                  <button
                    key={y}
                    type="button"
                    role="option"
                    aria-selected={y === year}
                    className={controls.opt}
                    onClick={() => { go({ year: y, wd: undefined }); setMenu(null) }}
                  >
                    {y} 年
                    <CheckIcon />
                  </button>
                ))}
              </div>
            ) : null
          }
        </MenuField>

        <MenuField
          label="季度"
          value={`${SEASON_LABEL[season]}季`}
          isSet
          open={menu === 'season'}
          onToggle={() => setMenu(menu === 'season' ? null : 'season')}
          onClose={() => setMenu(null)}
          popup="listbox"
        >
          {({ labelId }) =>
            menu === 'season' ? (
              <div className={controls.menu} role="listbox" aria-labelledby={labelId}>
                {SEASONS.map((s) => (
                  <button
                    key={s}
                    type="button"
                    role="option"
                    aria-selected={s === season}
                    className={controls.opt}
                    onClick={() => { go({ season: s, wd: undefined }); setMenu(null) }}
                  >
                    {SEASON_LABEL[s]}季
                    <CheckIcon />
                  </button>
                ))}
              </div>
            ) : null
          }
        </MenuField>

        <MenuField
          label="形式"
          value={search.format ? FORMAT_LABELS[search.format] ?? search.format : '任意'}
          isSet={Boolean(search.format)}
          open={menu === 'format'}
          onToggle={() => setMenu(menu === 'format' ? null : 'format')}
          onClose={() => setMenu(null)}
          popup="listbox"
        >
          {({ labelId }) =>
            menu === 'format' ? (
              <div className={controls.menu} role="listbox" aria-labelledby={labelId}>
                {['', ...FORMATS].map((f) => (
                  <button
                    key={f || 'any'}
                    type="button"
                    role="option"
                    aria-selected={(search.format ?? '') === f}
                    className={controls.opt}
                    onClick={() => { go({ format: f || undefined }); setMenu(null) }}
                  >
                    {f ? FORMAT_LABELS[f] ?? f : '任意'}
                    <CheckIcon />
                  </button>
                ))}
              </div>
            ) : null
          }
        </MenuField>

        <div className={controls.moreField} data-menu="more">
          <button
            type="button"
            className={controls.moreBtn}
            aria-label="更多筛选"
            aria-expanded={menu === 'more'}
            data-set={moreSet ? 'true' : undefined}
            onClick={() => setMenu(menu === 'more' ? null : 'more')}
          >
            <SlidersIcon />
          </button>
          {menu === 'more' && (
            <div className={controls.extra} role="group" aria-label="更多筛选">
              <div className={controls.extraGroup} role="group" aria-label="播出状态">
                <div className={controls.extraLabel} aria-hidden="true">播出状态</div>
                <div className={controls.chips}>
                  {['', ...STATUSES].map((s) => (
                    <button
                      key={s || 'all'}
                      type="button"
                      className={controls.chip}
                      aria-pressed={(search.status ?? '') === s}
                      onClick={() => go({ status: search.status === s || !s ? undefined : s })}
                    >
                      {s ? STATUS_LABELS[s] : '全部'}
                    </button>
                  ))}
                </div>
              </div>
              <div className={controls.extraGroup} role="group" aria-label="原作来源">
                <div className={controls.extraLabel} aria-hidden="true">原作来源</div>
                <div className={controls.chips}>
                  {['', ...SOURCES].map((src) => (
                    <button
                      key={src || 'all'}
                      type="button"
                      className={controls.chip}
                      aria-pressed={(search.source ?? '') === src}
                      onClick={() => go({ source: search.source === src || !src ? undefined : src })}
                    >
                      {src ? SOURCE_LABELS[src] : '全部'}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          )}
        </div>
      </section>

      {/* 手机搜索与展开筛选 */}
      <div className={controls.searchRow}>
        <div className={`${controls.control} ${controls.search}`}>
          <SearchIcon />
          <input
            type="search"
            aria-label="本季内搜索"
            placeholder="搜本季作品…"
            value={search.q ?? ''}
            onChange={(e) => go({ q: e.currentTarget.value || undefined })}
          />
        </div>
        <button
          type="button"
          className={controls.sheetBtn}
          aria-label="展开筛选"
          aria-expanded={mobileFiltersOpen}
          onClick={() => setMobileFiltersOpen((v) => !v)}
        >
          <SlidersIcon />
          {activeFiltersCount > 0 ? <span className={controls.sheetBadge}>{activeFiltersCount}</span> : null}
        </button>
      </div>
      <div className={controls.mobilePanel} data-open={mobileFiltersOpen ? 'true' : undefined}>
        <div className={controls.extraGroup} role="group" aria-label="季度">
          <div className={controls.extraLabel}>季度</div>
          <div className={controls.chips}>
            {SEASONS.map((s) => (
              <button key={s} type="button" className={controls.chip} aria-pressed={s === season} onClick={() => go({ season: s, wd: undefined })}>
                {SEASON_LABEL[s]}季
              </button>
            ))}
          </div>
        </div>
        <div className={controls.extraGroup} role="group" aria-label="类型">
          <div className={controls.extraLabel}>类型</div>
          <div className={controls.chips}>
            {GENRES.map((g) => (
              <button
                key={g}
                type="button"
                className={controls.chip}
                aria-pressed={(search.genre ?? '全部') === g}
                onClick={() => go({ genre: g === '全部' ? undefined : g })}
              >
                {g}
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* 手机色调横排 */}
      <BrowseHueDots options={hueOptions} selected={search.hue ?? null} onPick={(hue) => go({ hue: hue ?? undefined })} phone />

      {/* 标签行 + 桌面色调圆点 + 排序菜单 + 图标视图切换 */}
      <div className={tags.tags}>
        <div className={tags.tagsLeft}>
          <TagsIcon className={tags.tagIcon} />
          <span className={`${tags.tag} ${tags.tagFixed}`}>{year}</span>
          <span className={`${tags.tag} ${tags.tagFixed}`}>{SEASON_LABEL[season]}季</span>
          {search.q?.trim() && (
            <button type="button" className={tags.tag} onClick={() => go({ q: undefined })}>
              搜索：{search.q.trim()}
              <CloseIcon />
            </button>
          )}
          {search.genre && (
            <button type="button" className={tags.tag} onClick={() => go({ genre: undefined })}>
              {search.genre}
              <CloseIcon />
            </button>
          )}
          {search.format && (
            <button type="button" className={tags.tag} onClick={() => go({ format: undefined })}>
              {FORMAT_LABELS[search.format] ?? search.format}
              <CloseIcon />
            </button>
          )}
          {search.status && (
            <button type="button" className={tags.tag} onClick={() => go({ status: undefined })}>
              {STATUS_LABELS[search.status] ?? search.status}
              <CloseIcon />
            </button>
          )}
          {search.source && (
            <button type="button" className={tags.tag} onClick={() => go({ source: undefined })}>
              {SOURCE_LABELS[search.source] ?? search.source}
              <CloseIcon />
            </button>
          )}
          {selectedHueObj && (
            <button type="button" className={tags.tag} onClick={() => go({ hue: undefined })}>
              {selectedHueObj.label}
              <CloseIcon />
            </button>
          )}
          {typeof search.wd === 'number' && (
            <button type="button" className={tags.tag} onClick={() => go({ wd: undefined })}>
              {WEEKDAY_LONG[search.wd]}播出
              <CloseIcon />
            </button>
          )}
          {hasFilters && (
            <button type="button" className={`${tags.tag} ${tags.tagClear}`} onClick={clearFilters}>
              清除全部
              <CloseIcon />
            </button>
          )}
        </div>

        <div className={tags.tagsRight}>
          <BrowseHueDots options={hueOptions} selected={search.hue ?? null} onPick={(hue) => go({ hue: hue ?? undefined })} />

          <div className={tags.sortWrap} data-menu="sort">
            <button
              ref={sortTriggerRef}
              type="button"
              className={tags.sort}
              aria-haspopup="listbox"
              aria-expanded={menu === 'sort'}
              data-sort-value={sort}
              onClick={() => setMenu(menu === 'sort' ? null : 'sort')}
            >
              <SortIcon />
              {SORT_LABEL[sort]}
            </button>
            {menu === 'sort' && (
              <div className={`${controls.menu} ${controls.menuRight}`} role="listbox" aria-label="排序">
                {SORTS.map((key) => (
                  <button
                    key={key}
                    type="button"
                    role="option"
                    aria-selected={sort === key}
                    className={controls.opt}
                    onClick={() => {
                      go({ sort: key === 'score' ? undefined : key })
                      setMenu(null)
                      sortTriggerRef.current?.focus({ preventScroll: true })
                    }}
                  >
                    {SORT_LABEL[key]}
                    <CheckIcon />
                  </button>
                ))}
              </div>
            )}
          </div>

          <div className={tags.views} role="group" aria-label="切换视图">
            <button
              type="button"
              className={tags.view}
              aria-label="封面网格"
              title="封面网格"
              aria-pressed={view === 'grid'}
              data-view-toggle="grid"
              onClick={() => go({ view: undefined })}
            >
              <GridIcon />
            </button>
            <button
              type="button"
              className={tags.view}
              aria-label="图表卡片"
              title="图表卡片"
              aria-pressed={view === 'chart'}
              data-view-toggle="chart"
              onClick={() => go({ view: 'chart' })}
            >
              <ChartIcon />
            </button>
            <button
              type="button"
              className={tags.view}
              aria-label="列表视图"
              title="列表视图"
              aria-pressed={view === 'table'}
              data-view-toggle="table"
              onClick={() => go({ view: 'table' })}
            >
              <ListIcon />
            </button>
          </div>
        </div>
      </div>

      {resource.loading && (
        <div className={frame.skeletonGrid} role="status" aria-busy="true">
          <span className="visually-hidden">正在读取 {year} 年{SEASON_LABEL[season]}季</span>
          {Array.from({ length: 12 }, (_, i) => <div key={i} className={frame.skeletonCover} aria-hidden="true" />)}
        </div>
      )}
      {resource.error && (
        <div className={frame.noticeWrap}>
          <CatalogNotice
            tone="error"
            title="季度目录读取失败"
            actions={<button type="button" className="btn" onClick={resource.retry}>重试</button>}
          >
            {resource.error}
          </CatalogNotice>
        </div>
      )}
      {items && filtered.length > 0 && <BrowseResults items={filtered} view={view} />}
      {items && filtered.length === 0 && (
        <div className={frame.noticeWrap}>
          {items.length === 0 ? (
            <CatalogNotice title="这一季暂时没有作品">目录里还没有这一季的条目，换一季看看。</CatalogNotice>
          ) : (
            <CatalogNotice
              title="没有符合筛选的作品"
              actions={<button type="button" className="btn" onClick={clearFilters}>清除筛选</button>}
            >
              放宽类型、形式、色调或状态的条件再看看。
            </CatalogNotice>
          )}
        </div>
      )}
    </main>
  )
}
