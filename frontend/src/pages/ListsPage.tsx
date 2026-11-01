import { useState } from 'react'
import { CatalogNotice, PosterSkeletons } from '../components/catalog/CatalogStates'
import { RouteLink } from '../components/catalog/RouteLink'
import { DiscoverCard } from '../components/media/DiscoverCard'
import { useCollection } from '../components/media/CollectionContext'
import { Tab, TabCount, Tabs } from '../components/ui'
import { collectionMedia, COLLECTION_LABELS, entryStatus } from '../lib/media'
import type { CollectionStatus } from '../lib/media'
import '../components/catalog/catalog.css'
import '../components/media/media.css'

/** 账号里的四种状态，顺序与账号服务一致 */
const ORDER: readonly CollectionStatus[] = ['watching', 'plan_to_watch', 'completed', 'dropped']

/** 我的追番（`/lists`）：账号收藏按四种状态分段，每段一张海报网格 */
export function ListsPage() {
  const [status, setStatus] = useState<CollectionStatus>('watching')
  const collection = useCollection()
  const items = collection.entries.filter(entry => entryStatus(entry) === status)
  const retry = () => void collection.reload()

  return <main className="catalog-page">
    <header className="catalog-head">
      <h1 className="catalog-title">我的追番</h1>
      {collection.loggedIn && <div className="catalog-segments">
        <Tabs value={status} onChange={next => setStatus(next as CollectionStatus)} label="观看状态">
          {ORDER.map(value => <Tab key={value} value={value}>{COLLECTION_LABELS[value]}<TabCount n={collection.entries.filter(entry => entryStatus(entry) === value).length} /></Tab>)}
        </Tabs>
      </div>}
    </header>
    {/* 已经显示着列表时刷新失败：留着列表，只在上面说一句 */}
    {collection.error && collection.entries.length > 0 && <p className="result result--err" role="alert">{collection.error} <button type="button" className="link" onClick={retry}>重试</button></p>}
    {collection.loading
      ? <PosterSkeletons label="正在读取追番列表" />
      : collection.error && collection.entries.length === 0
        ? <CatalogNotice tone="error" title="追番列表读取失败" actions={<button type="button" className="btn" onClick={retry}>重试</button>}>{collection.error}</CatalogNotice>
        : !collection.loggedIn
          ? <CatalogNotice title="登录后显示你的追番" actions={<RouteLink to="/settings#account" className="btn btn--primary">去登录</RouteLink>}>登录账号后，这里会列出在看、想看、看完和弃番的作品，看完的集数也会同步过来。</CatalogNotice>
          : items.length === 0
            ? <CatalogNotice title={`「${COLLECTION_LABELS[status]}」里还没有作品`} actions={<RouteLink to="/" className="btn">去发现</RouteLink>}>在作品卡或作品页上点「+」，就能把它加进追番。</CatalogNotice>
            : <ul className="catalog-grid">{items.map(entry => <DiscoverCard key={entry.anilistId} media={collectionMedia(entry)} />)}</ul>}
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
