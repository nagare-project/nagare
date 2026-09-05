import { useMemo, useState } from 'react'
import { PosterGrid } from './PosterGrid'
import { Icon } from '../ui/Icon'
import type { LibraryCluster } from '../../lib/endpoints'

/** 只筛选已扫描的真实文件，筛选与排序不修改库或播放进度。 */
export function LibraryCollection({ clusters }: { clusters: LibraryCluster[] }) {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState('all')
  const [sort, setSort] = useState('default')
  const visible = useMemo(() => {
    const matches = clusters.filter((cluster) => {
      if (!cluster.title.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())) return false
      const items = cluster.groups.flatMap((g) => g.items)
      if (filter === 'watching') return items.some((i) => i.progress && (i.progress.completed || i.progress.positionSec > 0)) && items.some((i) => !i.progress?.completed)
      if (filter === 'unwatched') return items.every((i) => !i.progress || (!i.progress.completed && i.progress.positionSec === 0))
      if (filter === 'completed') return items.length > 0 && items.every((i) => i.progress?.completed)
      return true
    })
    if (sort === 'title') matches.sort((a, b) => a.title.localeCompare(b.title, 'zh-CN'))
    if (sort === 'episodes') matches.sort((a, b) => b.episodeCount - a.episodeCount)
    return matches
  }, [clusters, query, filter, sort])

  return <section className="library-collection" aria-labelledby="collection-heading">
    <div className="collection-head">
      <h2 id="collection-heading" className="section-title">本地媒体库 <span className="section-count">{clusters.length}</span></h2>
      <div className="collection-tools">
        <label className="collection-search"><Icon name="search" size={17} /><input className="input" type="search" aria-label="筛选媒体库" placeholder="搜索媒体库…" value={query} onChange={(e) => setQuery(e.target.value)} /></label>
        <select className="input collection-sort" aria-label="媒体库排序" value={sort} onChange={(e) => setSort(e.target.value)}>
          <option value="default">默认排序</option><option value="title">名称</option><option value="episodes">集数最多</option>
        </select>
      </div>
    </div>
    <div className="collection-filters" aria-label="媒体库筛选">
      {([['all', '全部'], ['watching', '在看'], ['unwatched', '未开始'], ['completed', '已看完']] as const).map(([value, text]) =>
        <button type="button" key={value} className="collection-filter" aria-pressed={filter === value} onClick={() => setFilter(value)}>{text}</button>)}
    </div>
    {visible.length ? <PosterGrid clusters={visible} /> : <p className="collection-empty" role="status">没有符合条件的作品。试试其他名称或观看状态。</p>}
  </section>
}
