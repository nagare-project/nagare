import { CarouselRow } from '../components/media/CarouselRow'
import { MediaCard } from '../components/media/MediaCard'
import { FixtureNotice } from './ListsPage'
import { FAKE_DISCOVER } from '../lib/fixtures/library'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/discover` 发现页：按板块横向排列的作品。
 *
 * FIXME(G2): 假数据。真数据是元数据（读），在允许的三条连线内 ——
 * 但这一页【绝不能】出现「按 anilistId 要磁力」的入口，那是红线 2。
 * 缺口见 todos.md。
 */
export function DiscoverPage() {
  return (
    <main className="lib-shell">
      <header className="page-head">
        <h1 className="page-title">发现</h1>
      </header>

      <FixtureNotice gap="G2" what="榜单" />

      <CarouselRow title="本季热门" subtitle="正在播出、讨论度最高的">
        {FAKE_DISCOVER.trending.map((m) => (
          <MediaCard key={m.id} media={m} />
        ))}
      </CarouselRow>

      <CarouselRow title="高人气" subtitle="历年评分与收藏最高的">
        {FAKE_DISCOVER.popular.map((m) => (
          <MediaCard key={m.id} media={m} />
        ))}
      </CarouselRow>

      <CarouselRow title="即将播出">
        {FAKE_DISCOVER.upcoming.map((m) => (
          <MediaCard key={m.id} media={m} />
        ))}
      </CarouselRow>
    </main>
  )
}
