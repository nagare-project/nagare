import { useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { needsConfirmation } from '../../lib/associations'
import type { LibrarySeries } from '../../lib/librarySeries'
import { Icon } from '../ui/Icon'

/**
 * 媒体库末尾的「还没认出的 N 组」（DESIGN.md「媒体库 ③」）：默认折叠，每组的主操作是「认作品」——
 * 点进这个分组的页面，在那里选对应的目录作品（选作品对话框、保存与重读都在那一页，只有一条路）。
 * 网格里一部作品都没有时默认展开：否则整个媒体库看起来是空的。
 */
export function UnrecognizedGroups({ series, openByDefault }: { series: LibrarySeries[]; openByDefault: boolean }) {
  const [open, setOpen] = useState(openByDefault)
  // 筛选之后网格空了：自动展开（用户自己收起之后网格又有了内容，不替他再收起）
  useEffect(() => { if (openByDefault) setOpen(true) }, [openByDefault])

  return (
    <details className="unrec" open={open} onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary className="unrec-summary">
        <span className="lib-section-title">还没认出的 {series.length} 组</span>
        <span className="unrec-summary-hint">认出作品之后才有海报、弹幕和观看同步</span>
      </summary>
      <ul className="unrec-list">
        {series.map((s) => {
          const cluster = s.clusters[0]!
          const meta = [s.season !== null ? `第 ${s.season} 季` : null, `${s.episodeCount} 集`].filter(Boolean).join(' · ')
          return (
            <li key={s.key}>
              <Link to="/anime/$clusterKey" params={{ clusterKey: cluster.clusterKey }} className="unrec-hit">
                <span className="unrec-thumb" aria-hidden="true">流</span>
                <span className="unrec-text">
                  <span className="unrec-title">{s.title}</span>
                  <span className="unrec-meta">{meta}</span>
                </span>
                {needsConfirmation(cluster) && <span className="status-badge status-badge--warning">待确认</span>}
                <span className="unrec-action">认作品<Icon name="right" size={14} /></span>
              </Link>
            </li>
          )
        })}
      </ul>
    </details>
  )
}
