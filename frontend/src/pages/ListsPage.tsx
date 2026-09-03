import { useState } from 'react'
import { MediaCard } from '../components/media/MediaCard'
import { FAKE_LISTS, LIST_STATUS_LABEL } from '../lib/fixtures/library'
import type { ListStatus } from '../lib/fixtures/library'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/lists` 我的列表：按观看状态分档的作品网格。
 *
 * FIXME(G1): 现在吃的是假数据。真数据要一个「读收藏列表」的接口，
 * 而 animego 侧目前只有【写】进度（MarkWatched），没有读。缺口见 todos.md。
 */

const ORDER: readonly ListStatus[] = ['watching', 'planning', 'completed', 'paused', 'dropped']

export function ListsPage() {
  const [status, setStatus] = useState<ListStatus>('watching')
  const items = FAKE_LISTS[status]

  return (
    <main className="lib-shell">
      <header className="page-head">
        <h1 className="page-title">我的列表</h1>
      </header>

      <FixtureNotice gap="G1" what="收藏列表" />

      <nav className="tabs" aria-label="观看状态">
        {ORDER.map((s) => (
          <button
            key={s}
            type="button"
            className={s === status ? 'tab tab--on' : 'tab'}
            aria-current={s === status ? 'true' : undefined}
            onClick={() => setStatus(s)}
          >
            {LIST_STATUS_LABEL[s]}
            <span className="tab-count">{FAKE_LISTS[s].length}</span>
          </button>
        ))}
      </nav>

      {items.length === 0 ? (
        <p className="result result--dim">这一档还没有作品。</p>
      ) : (
        <ul className="poster-grid">
          {items.map((m) => (
            <MediaCard key={m.id} media={m} />
          ))}
        </ul>
      )}
    </main>
  )
}

/**
 * 假数据横幅。存在的理由：演示界面时必须一眼看得出哪些数字是真的。
 * 不写这一条，截图发出去别人会当成功能已经做好了。
 */
export function FixtureNotice({ gap, what }: { gap: string; what: string }) {
  return (
    <p className="alert-warn" role="status">
      本页的{what}是<strong>假数据</strong>，用于界面演示。真接口尚未接通（缺口 {gap}，见仓库根
      todos.md）。
    </p>
  )
}
