import { useState } from 'react'
import { CarouselRow } from '../components/media/CarouselRow'
import { DiscoverHero } from '../components/media/DiscoverHero'
import { MediaCard } from '../components/media/MediaCard'
import { FixtureNotice } from './ListsPage'
import { SchedulePage } from './SchedulePage'
import { FAKE_DISCOVER, FAKE_FEATURED } from '../lib/fixtures/library'
import type { FakeMedia } from '../lib/fixtures/types'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/discover` 发现页 —— 对齐 seanime 的 anime 标签页。
 *
 * 板块顺序照它来：热门 → 最近更新 → 本季 → 上季 → 补番 → 即将播出 → 剧场版。
 * 标签只有「动画 / 放送表」两个：漫画那一档用户明确排除了。
 *
 * FIXME(G2): 全部是假数据。真数据是元数据（读），在允许的三条连线内 ——
 * 但这一页【绝不能】出现「按 anilistId 要磁力」的入口，那是红线 2。
 * 缺口见仓库根 todos.md。
 */

type Tab = 'anime' | 'schedule'

const SECTIONS: ReadonlyArray<{ key: keyof typeof FAKE_DISCOVER; title: string; sub?: string }> = [
  { key: 'trending', title: '本季热门', sub: '正在播出、讨论度最高的' },
  { key: 'recent', title: '最近更新' },
  { key: 'thisSeason', title: '本季新番' },
  { key: 'pastSeason', title: '上季作品' },
  { key: 'missedSequels', title: '错过的续作', sub: '你看过前作，但还没开始的' },
  { key: 'upcoming', title: '即将播出' },
  { key: 'movies', title: '剧场版' },
]

export function DiscoverPage() {
  const [tab, setTab] = useState<Tab>('anime')

  return (
    <main className="lib-shell discover-shell">
      <DiscoverHero items={FAKE_FEATURED} />

      <nav className="tabs tabs--center" aria-label="发现分类">
        <button
          type="button"
          className={tab === 'anime' ? 'tab tab--on' : 'tab'}
          aria-current={tab === 'anime' ? 'true' : undefined}
          onClick={() => setTab('anime')}
        >
          动画
        </button>
        <button
          type="button"
          className={tab === 'schedule' ? 'tab tab--on' : 'tab'}
          aria-current={tab === 'schedule' ? 'true' : undefined}
          onClick={() => setTab('schedule')}
        >
          放送表
        </button>
      </nav>

      {tab === 'schedule' ? (
        // 放送表整页复用，不重写一份 —— 它在 /schedule 也是同一个东西
        <SchedulePage embedded />
      ) : (
        <>
          <FixtureNotice gap="G2" what="榜单" />
          {SECTIONS.map((s) => (
            <Section key={s.key} title={s.title} sub={s.sub} items={FAKE_DISCOVER[s.key]} />
          ))}
        </>
      )}
    </main>
  )
}

function Section({ title, sub, items }: { title: string; sub?: string; items: FakeMedia[] }) {
  if (items.length === 0) return null
  return (
    <CarouselRow title={title} subtitle={sub}>
      {items.map((m) => (
        <MediaCard key={`${title}-${m.id}`} media={m} />
      ))}
    </CarouselRow>
  )
}
