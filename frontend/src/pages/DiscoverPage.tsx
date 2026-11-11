import { useMemo, useState } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { CatalogNotice } from '../components/catalog/CatalogStates'
import { EmptyLibraryStrip } from '../components/catalog/EmptyLibraryStrip'
import { FreshnessNote } from '../components/catalog/FreshnessNote'
import { RouteLink } from '../components/catalog/RouteLink'
import { GemsPanel } from '../components/home/GemsPanel'
import { HomeHero } from '../components/home/HomeHero'
import { HomeHueScope } from '../components/home/HomeHueScope'
import { HomeSkeleton } from '../components/home/HomeSkeleton'
import { HueBrowser, type HueFamilyView } from '../components/home/HueBrowser'
import { SeasonTopGrid } from '../components/home/SeasonTopGrid'
import { TodayRail } from '../components/home/TodayRail'
import { TrendingRanks } from '../components/home/TrendingRanks'
import { YearTopList } from '../components/home/YearTopList'
import { useHomeClock } from '../components/home/useHomeClock'
import { CatalogSearchForm, CatalogSearchResults } from '../components/media/CatalogSearch'
import { useCatalogResource } from '../hooks/useCatalogResource'
import { fetchDiscoverData } from '../lib/endpoints'
import type { DiscoverData } from '../lib/endpoints'
import { colouredFirst, defaultFamily, groupByHueFamily } from '../lib/home/hueFamilies'
import { seasonYearLabel } from '../lib/home/labels'
import { clockParts, dayHeader } from '../lib/home/time'
import { airingsOnDay, dayAfter } from '../lib/home/todaySlots'
import { airingsOf, gemCard, heroSlide, hueCard, seasonCard, todayCard, trendCard, yearCard, type TodayCard } from '../lib/home/viewModels'
import hueScope from '../components/home/HomeHueScope.module.css'
import '../components/catalog/catalog.css'
import '../components/media/media.css'
import '../components/media/discover.css'

/** 横幅取本季前几部 */
const HERO_COUNT = 5
/** 本季高分取前几部 */
const SEASON_TOP_COUNT = 12
/** 大家都在追取前几部 */
const TRENDING_COUNT = 10

/**
 * 发现页（`/`，DESIGN.md「发现」）：横幅「本季焦点」+ 今日更新 + 本季高分 + 按色调逛 + 大家都在追 +
 * 完结佳作与年度榜。整页穿着横幅当前那一部的颜色（HomeHueScope）。地址栏带 ?q= 时换成作品搜索结果。
 */
export function DiscoverPage() {
  // 发现页挂在 `/`（`/discover` 是跳到这里的别名）
  const search = useSearch({ from: '/' })
  const navigate = useNavigate({ from: '/' })
  // 根路由不校验参数，地址栏原样的 q 会一路传下来（?q=%20%20 到这里是两个空格）：在这里再收一次
  const query = (search.q ?? '').trim()
  // 同一个词再点「搜索」时地址栏不变：用这个序号让结果重新拉一次（后端有缓存，不会多打上游）
  const [searchRun, setSearchRun] = useState(0)
  // 搜索时目录也照常读着：清掉关键词就能立刻回到发现页
  const discover = useCatalogResource('discover', fetchDiscoverData, { fallbackError: '目录服务没有回应' })

  function handleSearch(q: string) {
    setSearchRun((n) => n + 1)
    void navigate({ search: (prev) => ({ ...prev, q: q === '' ? undefined : q }) })
  }

  if (query !== '') {
    return (
      <main className="discover-page">
        {/* 版面上没有页面标题，读屏仍要一个 h1：按标题跳转时知道这是哪一页，关掉顶部说明后焦点也落在这里 */}
        <h1 className="visually-hidden">发现</h1>
        <EmptyLibraryStrip />
        <div className="catalog-page">
          <CatalogSearchForm query={query} onSearch={handleSearch} />
          <CatalogSearchResults query={query} run={searchRun} />
        </div>
      </main>
    )
  }
  if (discover.data && hasContent(discover.data)) return <HomeView data={discover.data} refreshing={discover.refreshing} />
  if (discover.data || discover.error) {
    const failed = discover.error !== undefined || sectionErrors(discover.data).length > 0
    return (
      <main className="discover-page">
        <h1 className="visually-hidden">发现</h1>
        <EmptyLibraryStrip />
        <div className="catalog-page">
          {failed
            ? <CatalogUnreachable detail={discover.error} retry={discover.retry} />
            : <CatalogNotice title="暂时没有可显示的作品" actions={<button type="button" className="btn" onClick={discover.retry}>重新读取</button>}>目录服务这次没有返回任何作品，稍后再来看看。</CatalogNotice>}
        </div>
      </main>
    )
  }
  return <HomeSkeleton />
}

/** 有没有至少一块能显示；读取失败的块只藏那一块（DESIGN.md「发现」） */
function hasContent(data: DiscoverData): boolean {
  return [data.seasonal, data.trending, data.gems, data.yearly].some((s) => s.items.length > 0)
}

function sectionErrors(data: DiscoverData | undefined): string[] {
  if (!data) return []
  return [data.seasonal, data.trending, data.gems, data.yearly, data.schedule].flatMap((s) => (s.error ? [s.error] : []))
}

function HomeView({ data, refreshing }: { data: DiscoverData; refreshing: boolean }) {
  // 拼好的各段只在数据变了时重拼：随时钟变的部分（横幅的放送状态、今日更新、浮层里的下一集）各自读时钟，
  // 时钟每分钟走一格时不必把整页一百多张卡重画一遍
  const view = useMemo(() => {
    const schedule = data.schedule.items
    const seasonal = data.seasonal.items
    const hueGroups = groupByHueFamily(seasonal.map(hueCard))
    return {
      // 横幅仍是本季前五，但第一张换成有颜色的：第一张给整页上色，没有颜色的封面会让发现页一打开是灰的
      slides: colouredFirst(seasonal.slice(0, HERO_COUNT).map((row) => heroSlide(row, schedule))),
      seasonTop: seasonal.slice(0, SEASON_TOP_COUNT).map((row) => seasonCard(row, airingsOf(schedule, row.anilistId))),
      families: hueGroups.map((g): HueFamilyView => ({ key: g.key, hue: g.hue, label: g.label, items: g.items })),
      defaultFamily: defaultFamily(hueGroups),
      hueCount: hueGroups.reduce((n, g) => n + g.items.length, 0),
      todayCards: schedule.map(todayCard),
      trending: data.trending.items.slice(0, TRENDING_COUNT).map(trendCard),
      gems: data.gems.items.map(gemCard),
      yearly: data.yearly.items.map((row, i) => yearCard(row, i + 1)),
    }
  }, [data])
  const hues = useMemo(() => view.slides.map((s) => s.hue), [view])

  return (
    <HomeHueScope hues={hues}>
      {/* 版面上没有页面标题（横幅就是第一屏），读屏仍要一个 h1 */}
      <h1 className="visually-hidden">发现</h1>
      <EmptyLibraryStrip />
      <div className={hueScope.status}>
        <FreshnessNote freshness={data.freshness} refreshing={refreshing} />
      </div>
      <HomeHero slides={view.slides} />
      {data.schedule.error ? null : <TodaySection cards={view.todayCards} />}
      <SeasonTopGrid items={view.seasonTop} seasonLabel={seasonYearLabel(data.season, data.year)} seasonSearch={SEASON_SEARCH} total={data.seasonTotal} />
      {view.families.length > 0 ? <HueBrowser total={view.hueCount} families={view.families} defaultKey={view.defaultFamily ?? view.families[0].key} /> : null}
      <TrendingRanks items={view.trending} />
      <div className={hueScope.pair}>
        <GemsPanel initial={view.gems} />
        <YearTopList items={view.yearly} year={data.year} />
      </div>
    </HomeHueScope>
  )
}

/** 季度页不带参数就是本季 */
const SEASON_SEARCH = {}

/** 今日更新：「今天」按本机的日子算（上游按它自己服务器的日子分组），过了零点自己换到新的一天 */
function TodaySection({ cards }: { cards: TodayCard[] }) {
  const todayKey = clockParts(useHomeClock()).dayKey
  const { today, tomorrow } = useMemo(() => {
    const next = dayAfter(cards, todayKey)
    return { today: airingsOnDay(cards, todayKey), tomorrow: next ? { label: dayHeader(next.dayKey), count: next.count } : null }
  }, [cards, todayKey])
  return <TodayRail items={today} dayLabel={dayHeader(todayKey)} tomorrow={tomorrow} />
}

/** 一份缓存都没有、又连不上目录服务（DESIGN.md「状态表 · 发现」） */
function CatalogUnreachable({ detail, retry }: { detail?: string; retry: () => void }) {
  return (
    <CatalogNotice tone="error" title="连不上目录服务，媒体库照常可用"
      actions={<>
        <RouteLink to="/library" className="btn btn--primary">去媒体库</RouteLink>
        <button type="button" className="btn" onClick={retry}>重试</button>
      </>}>
      {detail ?? '发现页的作品来自在线目录；本机的番剧文件不受影响。'}
    </CatalogNotice>
  )
}
