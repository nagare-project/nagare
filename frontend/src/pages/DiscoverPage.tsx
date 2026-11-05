import { useState } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { CatalogNotice } from '../components/catalog/CatalogStates'
import { EmptyLibraryStrip } from '../components/catalog/EmptyLibraryStrip'
import { FreshnessNote } from '../components/catalog/FreshnessNote'
import { RouteLink } from '../components/catalog/RouteLink'
import { adjacentSeason, currentSeason } from '../components/catalog/season'
import { CarouselRow } from '../components/media/CarouselRow'
import { CatalogSearchForm, CatalogSearchResults } from '../components/media/CatalogSearch'
import { DiscoverCard } from '../components/media/DiscoverCard'
import { DiscoverGenres, genreLabel, genreQuery } from '../components/media/DiscoverGenres'
import { DiscoverHero } from '../components/media/DiscoverHero'
import type { MediaSummary } from '../components/media/types'
import { useCatalogResource } from '../hooks/useCatalogResource'
import { fetchDiscover } from '../lib/media'
import type { DiscoverSections, DiscoverView } from '../lib/media'
import '../components/catalog/catalog.css'
import '../components/media/media.css'
import '../components/media/discover.css'

/** 横幅最多轮几部 */
const HERO_LIMIT = 12
/** 骨架里每行画几张卡 */
const SKELETON_CARDS = 6

/** 发现页（`/`，DESIGN.md「发现」）：横幅 + 海报行；地址栏带 ?q= 时换成作品搜索结果 */
export function DiscoverPage() {
  // 发现页挂在 `/`（`/discover` 是跳到这里的别名）
  const search = useSearch({ from: '/' })
  const navigate = useNavigate({ from: '/' })
  // 根路由不校验参数，地址栏原样的 q 会一路传下来（?q=%20%20 到这里是两个空格）：在这里再收一次
  const query = (search.q ?? '').trim()
  // 同一个词再点「搜索」时地址栏不变：用这个序号让结果重新拉一次（后端有缓存，不会多打上游）
  const [searchRun, setSearchRun] = useState(0)
  // 搜索时榜单也照常读着：清掉关键词就能立刻回到榜单
  const discover = useCatalogResource('discover', fetchDiscover, { fallbackError: '目录服务没有回应' })

  function handleSearch(q: string) {
    setSearchRun((n) => n + 1)
    void navigate({ search: (prev) => ({ ...prev, q: q === '' ? undefined : q }) })
  }

  return (
    <main className="discover-page">
      {/* 版面上没有页面标题（横幅就是第一屏），读屏仍要一个 h1：按标题跳转时知道这是哪一页，关掉顶部说明后焦点也落在这里 */}
      <h1 className="visually-hidden">发现</h1>
      <EmptyLibraryStrip />
      {query !== '' ? (
        <div className="catalog-page">
          <CatalogSearchForm query={query} onSearch={handleSearch} />
          <CatalogSearchResults query={query} run={searchRun} />
        </div>
      ) : discover.data ? (
        <DiscoverContent view={discover.data} refreshing={discover.refreshing} initialGenre={genreLabel(search.genre ?? '')} retry={discover.retry} />
      ) : discover.error ? (
        <div className="catalog-page"><CatalogUnreachable detail={discover.error} retry={discover.retry} /></div>
      ) : (
        <DiscoverSkeleton />
      )}
    </main>
  )
}

/** 板块有作品才画；读取失败的板块整块隐藏（DESIGN.md「发现」） */
function visibleSections(sections: DiscoverSections): DiscoverSections {
  return sections.filter((section) => section.items.length > 0 && !section.error)
}

/** 横幅：本季里已经开播的作品；本季一部都没有时退到第一个有作品的板块 */
function heroSource(sections: DiscoverSections): { label: string; items: MediaSummary[] } {
  const season = sections.find((section) => section.key === 'thisSeason' && !section.error)
  const released = season?.items.filter((item) => item.status !== 'NOT_YET_RELEASED') ?? []
  if (season && released.length > 0) return { label: season.title, items: released.slice(0, HERO_LIMIT) }
  const first = visibleSections(sections)[0]
  return { label: first?.title ?? '', items: first?.items.slice(0, HERO_LIMIT) ?? [] }
}

function DiscoverContent({ view, refreshing, initialGenre, retry }: { view: DiscoverView; refreshing: boolean; initialGenre: string; retry: () => void }) {
  const sections = visibleSections(view.sections)
  if (sections.length === 0) {
    const failed = view.sections.some((section) => section.error)
    return (
      <div className="catalog-page">
        {failed
          ? <CatalogUnreachable retry={retry} />
          : <CatalogNotice title="暂时没有可显示的作品" actions={<button type="button" className="btn" onClick={retry}>重新读取</button>}>目录服务这次没有返回任何板块，稍后再来看看。</CatalogNotice>}
      </div>
    )
  }
  const hero = heroSource(view.sections)
  return (
    <>
      <div className="discover-status"><FreshnessNote freshness={view.freshness} refreshing={refreshing} /></div>
      {hero.items.length > 0 && <DiscoverHero items={hero.items} label={hero.label} />}
      <div className="catalog-page discover-rows">
        {sections.map((section) => (
          <DiscoverSection key={section.key} sectionKey={section.key} title={section.title} items={section.items} initialGenre={section.key === 'trending' ? initialGenre : '全部'} />
        ))}
      </div>
    </>
  )
}

/** 本季 / 上季两行的「查看全部」：季度页里有完整列表与按类型筛选 */
function seasonLink(sectionKey: string): Record<string, string | number> | undefined {
  const now = currentSeason()
  if (sectionKey === 'thisSeason') return {}
  if (sectionKey === 'pastSeason') return adjacentSeason(now.season, now.year, -1)
  return undefined
}

function DiscoverSection({ sectionKey, title, items, initialGenre }: { sectionKey: string; title: string; items: MediaSummary[]; initialGenre: string }) {
  const [genre, setGenre] = useState(initialGenre)
  const filterable = sectionKey === 'trending'
  const visible = !filterable || genre === '全部' ? items : items.filter((media) => media.genres.includes(genreQuery(genre)))
  const season = seasonLink(sectionKey)
  return (
    <CarouselRow title={title}
      filters={filterable && <DiscoverGenres title={title} value={genre} onChange={setGenre} />}
      action={season && <RouteLink to="/seasonal" search={season} className="row-more">查看全部<span className="visually-hidden">：{title}</span></RouteLink>}>
      {visible.map((media) => <DiscoverCard key={`${media.id}-${media.recentAiring?.episode ?? ''}`} media={media} />)}
      {visible.length === 0 && <li className="discover-row-empty" role="status">当前返回的作品中暂无「{genre}」分类。</li>}
    </CarouselRow>
  )
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

/** 横幅 + 两行海报的静态骨架 */
function DiscoverSkeleton() {
  return (
    <div role="status" aria-busy="true">
      <span className="visually-hidden">正在读取发现页</span>
      <div className="discover-skeleton-hero" aria-hidden="true" />
      <div className="catalog-page discover-rows" aria-hidden="true">
        {[0, 1].map((row) => (
          <div key={row} className="discover-skeleton-row">
            <span className="discover-skeleton-title" />
            <ul className="catalog-grid">
              {Array.from({ length: SKELETON_CARDS }, (_, i) => (
                <li key={i} className="poster-skeleton"><span className="poster-skeleton-art" /><span className="poster-skeleton-line" /><span className="poster-skeleton-line poster-skeleton-line--short" /></li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </div>
  )
}
