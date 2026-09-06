import { useEffect, useState } from 'react'
import { AnimatePresence, domAnimation, LazyMotion, m } from 'motion/react'
import { CarouselRow } from '../components/media/CarouselRow'
import { DiscoverHero } from '../components/media/DiscoverHero'
import { DiscoverCard } from '../components/media/DiscoverCard'
import { DiscoverGenres, genreLabel, genreQuery } from '../components/media/DiscoverGenres'
import { useReducedMotionPreference } from '../components/media/useReducedMotionPreference'
import { Tab as TabButton, Tabs } from '../components/ui'
import { SchedulePage } from './SchedulePage'
import { fetchDiscover } from '../lib/media'
import type { DiscoverSections } from '../lib/media'
import { errorText } from '../lib/format'
import type { MediaSummary } from '../components/media/types'
import '../components/library/library.css'
import '../components/media/media.css'
import '../components/media/discover.css'

type Tab = 'anime' | 'schedule'


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
    void fetchDiscover(abort.signal).then(next => { if (!abort.signal.aborted) setData(next) })
      .catch(err => { if (!abort.signal.aborted) setError(errorText(err, '榜单加载失败')) })
    return () => abort.abort()
  }, [attempt])
  return <main className="lib-shell discover-shell">
    {data ? <DiscoverHero items={(data.find(section => section.key === 'thisSeason')?.items ?? []).filter(item => item.status !== 'NOT_YET_RELEASED').slice(0, 12)} showMetadata={tab === 'anime'} /> : <div className="hero discover-loading-hero" aria-label="正在加载精选作品" />}
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
              {data?.map(section => <Section key={section.key} title={section.title} items={section.items} filters={['trending', 'thisSeason', 'pastSeason'].includes(section.key)} arrows={section.key === 'recent'} initialGenre={section.key === 'trending' ? initialGenre : '全部'} error={section.error} retry={() => setAttempt(value => value + 1)} />)}
              {data && !data.some(section => section.items.length || section.error) && <p className="result" role="status">暂时没有可显示的作品。</p>}
            </>}
          </m.div>
        </AnimatePresence>
      </m.div>
    </LazyMotion>
  </main>
}

function Section({ title, items, filters, arrows, initialGenre, error, retry }: { title: string; items: MediaSummary[]; filters?: boolean; arrows?: boolean; initialGenre: string; error?: string; retry: () => void }) {
  const [genre, setGenre] = useState(initialGenre)
  const visible = genre === '全部' ? items : items.filter(media => media.genres.includes(genreQuery(genre)))
  return <CarouselRow title={title} autoPlay arrows={arrows} filters={filters && <DiscoverGenres title={title} value={genre} onChange={setGenre} />}>
    {visible.map(media => <DiscoverCard key={`${media.id}-${media.recentAiring?.episode ?? ''}`} media={media} />)}
    {error && <li className="discover-empty" role="alert"><span>{error} <button className="btn" onClick={retry}>重试</button></span></li>}
    {!error && visible.length === 0 && <li className="discover-empty" role="status">当前返回的作品中暂无「{genre}」分类。</li>}
  </CarouselRow>
}
