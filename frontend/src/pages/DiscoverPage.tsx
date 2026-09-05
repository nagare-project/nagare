import { useEffect, useState } from 'react'
import { AnimatePresence, domAnimation, LazyMotion, m } from 'motion/react'
import { CarouselRow } from '../components/media/CarouselRow'
import { DiscoverHero } from '../components/media/DiscoverHero'
import { DiscoverCard } from '../components/media/DiscoverCard'
import { DiscoverGenres, genreLabel, genreQuery } from '../components/media/DiscoverGenres'
import { useReducedMotionPreference } from '../components/media/useReducedMotionPreference'
import { Tab as TabButton, Tabs } from '../components/ui'
import { SchedulePage } from './SchedulePage'
import { fetchDiscover } from '../lib/catalog'
import type { DiscoverSections } from '../lib/catalog'
import { errorText } from '../lib/format'
import type { MediaSummary } from '../components/media/types'
import '../components/library/library.css'
import '../components/media/media.css'
import '../components/media/discover.css'

type Tab = 'anime' | 'schedule'
const SECTIONS = [
  { key: 'trending', title: '本季热门', filters: true },
  { key: 'recent', title: '最近更新', arrows: true },
  { key: 'thisSeason', title: '本季新番', filters: true },
  { key: 'pastSeason', title: '上季作品', filters: true },
  { key: 'missedSequels', title: '错过的续作' },
  { key: 'upcoming', title: '即将播出' },
  { key: 'movies', title: '剧场版' },
]

export function DiscoverPage() {
  const [tab, setTab] = useState<Tab>('anime')
  const [data, setData] = useState<DiscoverSections>()
  const [error, setError] = useState<string>()
  const [attempt, setAttempt] = useState(0)
  const initialGenre = genreLabel(new URLSearchParams(window.location.search).get('genre') || '')
  const reduced = useReducedMotionPreference()
  useEffect(() => {
    const abort = new AbortController()
    setError(undefined)
    void fetchDiscover('', '', abort.signal).then(next => { if (!abort.signal.aborted) setData(next) })
      .catch(err => { if (!abort.signal.aborted) setError(errorText(err, '榜单加载失败')) })
    return () => abort.abort()
  }, [attempt])
  return <main className="lib-shell discover-shell">
    {data ? <DiscoverHero items={(data.trending ?? []).filter(item => item.status !== 'NOT_YET_RELEASED').slice(0, 12)} showMetadata={tab === 'anime'} /> : <div className="hero discover-loading-hero" aria-label="正在加载精选作品" />}
    <LazyMotion features={domAnimation}>
      <m.div className="discover-content" initial={reduced ? false : { opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: .5, delay: .6 }}>
        <div className="discover-tabs">
          <Tabs value={tab} onChange={next => setTab(next as Tab)} label="发现分类" center>
            {(['anime', 'schedule'] as const).map(value => <TabButton key={value} value={value}>
              {tab === value && <m.span className="discover-tab-pill" layoutId="discover-category" transition={reduced ? { duration: 0 } : { type: 'spring', stiffness: 500, damping: 38 }} />}
              <span>{value === 'anime' ? '动画' : '放送表'}</span>
            </TabButton>)}
          </Tabs>
        </div>
        <AnimatePresence mode="wait" initial={false}>
          <m.div key={tab} className={tab === 'anime' ? 'discover-panel discover-anime-panel' : 'discover-panel'} initial={reduced ? false : { opacity: 0, y: 60 }} animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, scale: reduced ? 1 : .99 }} transition={{ duration: reduced ? 0 : .35 }}>
            {tab === 'schedule' ? <SchedulePage embedded /> : <>
              {error && <p className="result result--err" role="alert">{error} <button className="btn" onClick={() => setAttempt(value => value + 1)}>重新加载</button></p>}
              {!data && !error && <section aria-label="正在加载榜单" aria-busy="true"><div className="discover-loading-title" /><div className="discover-loading-cards">{[0, 1, 2, 3].map(i => <div key={i} />)}</div></section>}
              {data && SECTIONS.filter(section => data[section.key]?.length).map(({ key, ...section }) => <Section key={key} sectionKey={key} {...section} items={data[key]!} initialGenre={key === 'trending' ? initialGenre : '全部'} />)}
              {data && !Object.values(data).some(items => items.length) && <p className="result" role="status">暂时没有可显示的作品。</p>}
            </>}
          </m.div>
        </AnimatePresence>
      </m.div>
    </LazyMotion>
  </main>
}

function Section({ title, sectionKey, items, filters, arrows, initialGenre }: { title: string; sectionKey: string; items: MediaSummary[]; filters?: boolean; arrows?: boolean; initialGenre: string }) {
  const [genre, setGenre] = useState(initialGenre)
  const [filtered, setFiltered] = useState<MediaSummary[]>()
  const [error, setError] = useState<string>()
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    setFiltered(undefined); setError(undefined)
    if (genre === '全部') return
    const abort = new AbortController()
    void fetchDiscover(sectionKey, genreQuery(genre), abort.signal).then(next => { if (!abort.signal.aborted) setFiltered(next[sectionKey] ?? []) })
      .catch(err => { if (!abort.signal.aborted) setError(errorText(err, '筛选失败')) })
    return () => abort.abort()
  }, [genre, sectionKey, attempt])
  const visible = genre === '全部' ? items : filtered
  return <CarouselRow title={title} autoPlay arrows={arrows} filters={filters && <DiscoverGenres title={title} value={genre} onChange={setGenre} />}>
    {visible?.map(media => <DiscoverCard key={`${media.id}-${media.recentAiring?.episode ?? ''}`} media={media} />)}
    {!visible && !error && <li className="discover-empty" role="status">正在加载…</li>}
    {error && <li className="discover-empty" role="alert"><span>{error} <button className="btn" onClick={() => setAttempt(value => value + 1)}>重试</button></span></li>}
    {visible?.length === 0 && <li className="discover-empty" role="status">暂无「{genre}」作品。</li>}
  </CarouselRow>
}
