import { PosterCard } from './PosterCard'
import type { LibraryCluster } from '../../lib/endpoints'

/**
 * 媒体库的海报网格。列数随视口断点递增（照 seanime 的断点表），
 * 卡片自身不设固定宽度 —— 让网格分配，窄屏两列也不会挤成一条。
 */
export function PosterGrid({ clusters }: { clusters: LibraryCluster[] }) {
  return (
    <ul className="poster-grid" aria-label="媒体库">
      {clusters.map((cluster) => (
        <PosterCard key={cluster.clusterKey} cluster={cluster} />
      ))}
    </ul>
  )
}
