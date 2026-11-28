import { useEffect, useState, type CSSProperties } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { CatalogNotice } from '../components/catalog/CatalogStates'
import { FreshnessNote } from '../components/catalog/FreshnessNote'
import { SEASONS, SEASON_LABEL } from '../components/catalog/season'
import { BrowseResults, type BrowseViewMode } from '../components/browse/BrowseCards'
import { ChartIcon, ChevronIcon, GridIcon, ListIcon } from '../components/browse/icons'
import frame from '../components/browse/frame.module.css'
import top from '../components/browse/top.module.css'
import { GENRES, genreFilter, genreQuery } from '../components/media/genres'
import { FORMAT_LABELS } from '../components/media/media-format'
import { useCatalogResource } from '../hooks/useCatalogResource'
import { fetchTopData, fetchTopYears, type SeasonName } from '../lib/endpoints'

const TOP_FORMATS = ['TV', 'MOVIE', 'OVA', 'ONA', 'SPECIAL'] as const
const RECENT_YEAR_COUNT = 13

type OpenMenu = 'older' | 'genre' | null

function normalizeTopView(v?: string): BrowseViewMode {
  if (v === 'grid' || v === 'cover') return 'grid'
  if (v === 'chart') return 'chart'
  return 'table'
}

/**
 * `/top` 排行榜（1:1 对齐网站 TopBrowse）：
 * 40px 主标题 + 带滑动高亮块的等宽年份条（历代 + 近 13 年 + 更早 6 列浮层）+
 * 胶囊筛选行（形式 / 类型浮层 / 年度四季）+ 右侧图标视图切换（默认表格名次视图，可切 5 列封面墙与双列图表卡）。
 */
export function TopPage() {
  const search = useSearch({ from: '/top' })
  const navigate = useNavigate({ from: '/top' })

  const year = search.year
  const format = search.format
  const season = year ? (search.season as SeasonName | undefined) : undefined
  const genre = search.genre
  const view = normalizeTopView(search.view)
  const [menu, setMenu] = useState<OpenMenu>(null)

  const yearsRes = useCatalogResource('top-years', fetchTopYears, { fallbackError: '年份列表暂时无法读取' })
  const queryKey = `top:${year ?? 'all'}:${format ?? ''}:${season ?? ''}:${genre ?? ''}`
  const topRes = useCatalogResource(
    queryKey,
    (signal) => fetchTopData({ year, format, season, genre }, signal),
    { fallbackError: '排行榜暂时无法读取' },
  )

  const heading = year ? `${year} 年度 Top 100` : '全站 Top 100'

  useEffect(() => {
    const prev = document.title
    document.title = `${heading} · tomoru`
    return () => { document.title = prev }
  }, [heading])

  useEffect(() => {
    if (!menu) return
    const onDown = (e: PointerEvent) => {
      const target = e.target as Element | null
      if (target?.closest?.('[data-top-menu]')) return
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

  const patch = (next: Record<string, string | number | undefined>) => {
    setMenu(null)
    void navigate({ search: (prev) => ({ ...prev, ...next }) })
  }

  const allYears = yearsRes.data?.years.map((y) => y.year) ?? []
  const recentYears = allYears.slice(0, RECENT_YEAR_COUNT)
  const olderYears = allYears.slice(RECENT_YEAR_COUNT)
  const isOlderSelected = Boolean(year && !recentYears.includes(year))
  const hasOlder = olderYears.length > 0 || isOlderSelected
  const cells = 1 + recentYears.length + (hasOlder ? 1 : 0)
  const activeIdx = !year ? 0 : isOlderSelected ? cells - 1 : 1 + recentYears.indexOf(year)
  const stripStyle = { '--n': cells, '--i': Math.max(0, activeIdx) } as CSSProperties

  const items = topRes.data?.items ?? []
  const hasFilters = Boolean(format || season || genre)
  const genreOptions = GENRES.filter((g) => g !== '全部')
  const selectedGenreLabel = genre ? genreFilter(genre) ?? genre : null

  return (
    <main className={frame.page}>
      <header className={top.head}>
        <div className={top.titleRow}>
          <h1 className={top.h1}>{heading}</h1>
          <FreshnessNote freshness={topRes.data?.freshness} refreshing={topRes.refreshing} />
        </div>

        <div className={top.views} role="group" aria-label="切换视图">
          <button
            type="button"
            className={top.view}
            aria-label="封面网格"
            title="封面网格"
            aria-pressed={view === 'grid'}
            data-view-toggle="grid"
            onClick={() => patch({ view: 'grid' })}
          >
            <GridIcon />
          </button>
          <button
            type="button"
            className={top.view}
            aria-label="图表卡片"
            title="图表卡片"
            aria-pressed={view === 'chart'}
            data-view-toggle="chart"
            onClick={() => patch({ view: 'chart' })}
          >
            <ChartIcon />
          </button>
          <button
            type="button"
            className={top.view}
            aria-label="列表视图"
            title="列表视图"
            aria-pressed={view === 'table'}
            data-view-toggle="table"
            onClick={() => patch({ view: undefined })}
          >
            <ListIcon />
          </button>
        </div>

        <div className={top.stripWrap} data-top-menu>
          <div className={top.scroller}>
            <nav className={top.strip} style={stripStyle} aria-label="排行榜年份">
              <i className={top.ind} aria-hidden="true" />
              <button
                type="button"
                className={top.seg}
                data-on={!year ? 'true' : 'false'}
                aria-pressed={!year}
                onClick={() => patch({ year: undefined, season: undefined })}
              >
                历代
              </button>
              {recentYears.map((y) => (
                <button
                  key={y}
                  type="button"
                  className={top.seg}
                  data-on={year === y ? 'true' : 'false'}
                  data-mono="true"
                  aria-pressed={year === y}
                  onClick={() => patch({ year: y })}
                >
                  {y}
                </button>
              ))}
              {hasOlder && (
                <button
                  type="button"
                  className={top.seg}
                  aria-expanded={menu === 'older'}
                  data-on={isOlderSelected ? 'true' : 'false'}
                  data-mono={isOlderSelected ? 'true' : undefined}
                  onClick={() => setMenu(menu === 'older' ? null : 'older')}
                >
                  {isOlderSelected ? String(year) : '更早'}
                  <ChevronIcon className={top.segChev} />
                </button>
              )}
            </nav>
          </div>
          {hasOlder && (
            <div className={top.olderMenu} role="group" aria-label="更早年份" hidden={menu !== 'older'}>
              {olderYears.map((y) => (
                <button
                  key={y}
                  type="button"
                  className={top.olderOpt}
                  aria-pressed={year === y}
                  onClick={() => patch({ year: y })}
                >
                  {y}
                </button>
              ))}
            </div>
          )}
        </div>

        <div className={top.filters} role="group" aria-label="排行榜筛选">
          <div className={top.chips} role="group" aria-label="按形式筛选">
            <button
              type="button"
              className={top.chip}
              data-on={!format ? 'true' : 'false'}
              aria-pressed={!format}
              onClick={() => patch({ format: undefined })}
            >
              全部
            </button>
            {TOP_FORMATS.map((f) => (
              <button
                key={f}
                type="button"
                className={top.chip}
                data-on={format === f ? 'true' : 'false'}
                aria-pressed={format === f}
                onClick={() => patch({ format: format === f ? undefined : f })}
              >
                {FORMAT_LABELS[f] ?? f}
              </button>
            ))}
          </div>

          <i className={top.sep} aria-hidden="true" />

          <div className={top.pickWrap} data-top-menu>
            <button
              type="button"
              className={top.pick}
              aria-expanded={menu === 'genre'}
              data-on={selectedGenreLabel ? 'true' : 'false'}
              onClick={() => setMenu(menu === 'genre' ? null : 'genre')}
            >
              {selectedGenreLabel ?? '类型'}
              <ChevronIcon className={top.pickChev} />
            </button>
            <div className={top.genreMenu} role="group" aria-label="按类型筛选" hidden={menu !== 'genre'}>
              <button
                type="button"
                className={top.menuOpt}
                data-on={!genre ? 'true' : 'false'}
                onClick={() => patch({ genre: undefined })}
              >
                全部类型
              </button>
              {genreOptions.map((g) => {
                const q = genreQuery(g)
                return (
                  <button
                    key={g}
                    type="button"
                    className={top.menuOpt}
                    data-on={genre === q ? 'true' : 'false'}
                    onClick={() => patch({ genre: genre === q ? undefined : q })}
                  >
                    {g}
                  </button>
                )
              })}
            </div>
          </div>

          {year && (
            <>
              <i className={top.sep} aria-hidden="true" />
              <div className={top.chips} role="group" aria-label="按季度筛选">
                <button
                  type="button"
                  className={top.chip}
                  data-on={!season ? 'true' : 'false'}
                  aria-pressed={!season}
                  onClick={() => patch({ season: undefined })}
                >
                  全年
                </button>
                {SEASONS.map((s) => (
                  <button
                    key={s}
                    type="button"
                    className={top.chip}
                    data-on={season === s ? 'true' : 'false'}
                    aria-pressed={season === s}
                    onClick={() => patch({ season: season === s ? undefined : s })}
                  >
                    {SEASON_LABEL[s]}季
                  </button>
                ))}
              </div>
            </>
          )}
        </div>
      </header>

      {topRes.loading && (
        <div className={frame.skeletonRows} role="status" aria-busy="true">
          <span className="visually-hidden">正在读取排行榜</span>
          {Array.from({ length: 8 }, (_, i) => <div key={i} className={frame.skeletonRow} aria-hidden="true" />)}
        </div>
      )}
      {topRes.error && (
        <div className={frame.noticeWrap}>
          <CatalogNotice
            tone="error"
            title="排行榜暂时无法读取"
            actions={<button type="button" className="btn" onClick={topRes.retry}>重试</button>}
          >
            {topRes.error}
          </CatalogNotice>
        </div>
      )}
      {topRes.data && items.length === 0 && (
        <div className={frame.noticeWrap}>
          <CatalogNotice
            title="当前条件下暂无上榜作品"
            actions={
              hasFilters ? (
                <button type="button" className="btn" onClick={() => patch({ format: undefined, season: undefined, genre: undefined })}>
                  清除筛选
                </button>
              ) : undefined
            }
          >
            换一个年份或放宽筛选条件再看看。
          </CatalogNotice>
        </div>
      )}
      {topRes.data && items.length > 0 && <BrowseResults items={items} view={view} ranked />}
    </main>
  )
}
