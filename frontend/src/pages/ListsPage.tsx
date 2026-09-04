import { useState } from 'react'
import { MediaCard } from '../components/media/MediaCard'
import { Result, Tab, TabCount, Tabs } from '../components/ui'
import { FAKE_LISTS, LIST_STATUS_LABEL } from '../lib/fixtures/library'
import type { ListStatus } from '../lib/fixtures/library'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/lists` 我的列表：按观看状态分档的作品网格。
 *
 * FIXME(G1): 现在吃的是假数据。真数据要一个「读收藏列表」的接口，
 * 而 animego 侧目前只有【写】进度（MarkWatched），没有读。
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

      <Tabs
        value={status}
        onChange={(next) => setStatus(next as ListStatus)}
        label="观看状态"
      >
        {ORDER.map((s) => (
          <Tab key={s} value={s}>
            {LIST_STATUS_LABEL[s]}
            <TabCount n={FAKE_LISTS[s].length} />
          </Tab>
        ))}
      </Tabs>

      {items.length === 0 ? (
        <Result>这一档还没有作品。</Result>
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
 *
 * gap 是缺口编号（G1…G7），与代码里的 FIXME(Gn) 对得上。
 * 不在这里指向任何文档：缺口清单是内部文件，公开仓库里没有 ——
 * 给用户一个他打不开的链接比不给更糟。
 */
export function FixtureNotice({ gap, what }: { gap: string; what: string }) {
  return (
    <p className="alert-warn" role="status">
      本页的{what}是<strong>假数据</strong>，用于界面演示，真接口尚未接通（缺口 {gap}）。
    </p>
  )
}
