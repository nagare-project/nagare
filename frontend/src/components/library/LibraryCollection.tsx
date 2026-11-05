import { useMemo, useState } from 'react'
import { PosterGrid } from './PosterGrid'
import { UnrecognizedGroups } from './UnrecognizedGroups'
import { Icon } from '../ui/Icon'
import type { LibraryCluster } from '../../lib/endpoints'
import { clusterDisplayTitle } from '../../lib/associations'
import { groupSeries, seriesProgress } from '../../lib/librarySeries'
import type { LibrarySeries } from '../../lib/librarySeries'

const FILTERS = [['all', '全部'], ['watching', '在看'], ['unwatched', '未开始'], ['completed', '已看完']] as const
type Filter = (typeof FILTERS)[number][0]

/** 还没认出是哪部目录作品、用户也没下过决定（标为「不是目录里的作品」的算决定过，照常进网格） */
const isUnrecognized = (s: LibrarySeries) => s.work === undefined && s.clusters.every((c) => c.association === undefined)

/**
 * 媒体库的作品区（DESIGN.md「媒体库」②③）：认出的作品是 3:4 海报网格，同一部番的多个版本一张海报；
 * 还没认出的分组收进末尾默认折叠的「还没认出的 N 组」。后台还在认作品时，没认出的先以灰色占位
 * 留在网格里（「认作品中…」），认出后原地换成海报，不跳位。
 * 只筛选已扫描的真实文件，筛选与排序不修改库或播放进度。
 */
export function LibraryCollection({ clusters, identifying = false }: { clusters: LibraryCluster[]; identifying?: boolean }) {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [sort, setSort] = useState('default')
  const series = useMemo(() => groupSeries(clusters), [clusters])
  const visible = useMemo(() => {
    const q = query.trim().toLocaleLowerCase()
    const matches = series.filter((s) => {
      // 作品名、每个版本的文件夹标题都能搜到
      const titles = [s.title, ...s.clusters.flatMap((c) => [c.title, clusterDisplayTitle(c)])]
      if (!titles.some((t) => t.toLocaleLowerCase().includes(q))) return false
      // 按集算：同一集的几个版本里看完任意一个就算看完这一集
      const progress = seriesProgress(s.clusters)
      if (filter === 'watching') return progress.started > 0 && progress.completed < progress.total
      if (filter === 'unwatched') return progress.started === 0
      if (filter === 'completed') return progress.total > 0 && progress.completed === progress.total
      return true
    })
    if (sort === 'title') matches.sort((a, b) => a.title.localeCompare(b.title, 'zh-CN'))
    if (sort === 'episodes') matches.sort((a, b) => b.episodeCount - a.episodeCount)
    return matches
  }, [series, query, filter, sort])

  const grid = identifying ? visible : visible.filter((s) => !isUnrecognized(s))
  const pendingKeys = identifying ? new Set(visible.filter(isUnrecognized).map((s) => s.key)) : undefined
  const rest = identifying ? [] : visible.filter(isUnrecognized)
  const count = identifying ? series.length : series.filter((s) => !isUnrecognized(s)).length

  // 「还没认出的 N 组」展开与否记在这里而不是那一节自己身上：筛选把它整个藏掉时它会卸载，
  // 回来时用户自己的展开 / 收起不能丢。网格空了（一部都没认出、或筛选之后）就替用户展开 ——
  // 否则整个媒体库看起来是空的；网格又有内容时不替他收起
  const gridEmpty = grid.length === 0
  const [unrecognizedOpen, setUnrecognizedOpen] = useState(gridEmpty)
  const [sawGridEmpty, setSawGridEmpty] = useState(gridEmpty)
  if (gridEmpty !== sawGridEmpty) {
    setSawGridEmpty(gridEmpty)
    if (gridEmpty) setUnrecognizedOpen(true)
  }

  return (
    <section className="library-collection" aria-labelledby="collection-heading">
      <div className="collection-head">
        <h2 id="collection-heading" className="lib-section-title">作品 <span className="section-count">{count}</span></h2>
        <div className="collection-tools">
          <label className="collection-search">
            <Icon name="search" size={17} />
            <input className="input" type="text" inputMode="search" aria-label="筛选媒体库" placeholder="搜索媒体库…" value={query} onChange={(e) => setQuery(e.target.value)} />
          </label>
          <select className="input collection-sort" aria-label="媒体库排序" value={sort} onChange={(e) => setSort(e.target.value)}>
            <option value="default">默认排序</option><option value="title">名称</option><option value="episodes">集数最多</option>
          </select>
        </div>
      </div>
      <div className="collection-filters" role="group" aria-label="按观看状态筛选">
        {FILTERS.map(([value, text]) => (
          <button type="button" key={value} className="collection-filter" aria-pressed={filter === value} onClick={() => setFilter(value)}>{text}</button>
        ))}
      </div>
      {grid.length > 0 && <PosterGrid series={grid} {...(pendingKeys === undefined ? {} : { pendingKeys })} />}
      {grid.length === 0 && rest.length === 0 && <p className="collection-empty" role="status">没有符合条件的作品。试试其他名称或观看状态。</p>}
      {rest.length > 0 && <UnrecognizedGroups series={rest} open={unrecognizedOpen} onOpenChange={setUnrecognizedOpen} />}
    </section>
  )
}
