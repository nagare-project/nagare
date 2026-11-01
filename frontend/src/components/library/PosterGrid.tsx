import { PosterCard } from './PosterCard'
import type { LibrarySeries } from '../../lib/librarySeries'

/**
 * 媒体库的作品网格：桌面 6 列、平板 3 列、手机 2 列（DESIGN.md「布局」），卡片自身不设宽度，由网格分配。
 * pendingKeys：后台还在认、暂时没认出的作品（灰色占位 +「认作品中…」）。
 */
export function PosterGrid({ series, pendingKeys, label = '媒体库' }: { series: LibrarySeries[]; pendingKeys?: ReadonlySet<string>; label?: string }) {
  return (
    <ul className="tile-grid" aria-label={label}>
      {series.map((s) => (
        <PosterCard key={s.key} series={s} identifying={pendingKeys?.has(s.key) ?? false} />
      ))}
    </ul>
  )
}
