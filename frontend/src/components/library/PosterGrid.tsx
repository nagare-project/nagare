import { PosterCard } from './PosterCard'
import type { LibrarySeries } from '../../lib/librarySeries'

/**
 * 媒体库的海报网格。列数随视口断点递增（照 seanime 的断点表），
 * 卡片自身不设固定宽度 —— 让网格分配，窄屏两列也不会挤成一条。
 */
export function PosterGrid({ series }: { series: LibrarySeries[] }) {
  return (
    <ul className="poster-grid" aria-label="媒体库">
      {series.map((s) => (
        <PosterCard key={s.key} series={s} />
      ))}
    </ul>
  )
}
