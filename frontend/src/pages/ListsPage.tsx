import { useState } from 'react'
import { DiscoverCard } from '../components/media/DiscoverCard'
import { useCollection } from '../components/media/CollectionContext'
import { collectionMedia, COLLECTION_LABELS, entryStatus } from '../lib/media'
import type { CollectionStatus } from '../lib/media'
import { Result, Tab, TabCount, Tabs } from '../components/ui'
import '../components/library/library.css'
import '../components/media/media.css'
import '../components/media/discover.css'

const ORDER: readonly CollectionStatus[] = ['watching', 'plan_to_watch', 'completed', 'dropped']
export function ListsPage() {
  const [status, setStatus] = useState<CollectionStatus>('watching')
  const collection = useCollection()
  const items = collection.entries.filter(entry => entryStatus(entry) === status)
  return <main className="lib-shell">
    <header className="page-head"><h1 className="page-title">我的列表</h1></header>
    {collection.error && <p className="result result--err" role="alert">{collection.error} <button className="btn" onClick={() => void collection.reload()}>重试</button></p>}
    {collection.loading ? <Result>正在读取收藏…</Result> : !collection.loggedIn ? <Result>登录 animego 账号后查看收藏。<a className="link" href="/settings#account">登录账号</a></Result> : <>
      <Tabs value={status} onChange={next => setStatus(next as CollectionStatus)} label="观看状态">{ORDER.map(value => <Tab key={value} value={value}>{COLLECTION_LABELS[value]}<TabCount n={collection.entries.filter(entry => entryStatus(entry) === value).length} /></Tab>)}</Tabs>
      {!items.length ? <Result>这一档还没有作品。</Result> : <ul className="poster-grid">{items.map(entry => <DiscoverCard key={entry.anilistId} media={collectionMedia(entry)} />)}</ul>}
    </>}
  </main>
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
    <p className="alert-warn fixture-notice" role="status">
      本页的{what}是<strong>假数据</strong>，用于界面演示，尚未连接你的账号。<span className="visually-hidden">缺口 {gap}</span>
    </p>
  )
}
