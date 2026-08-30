import { useId } from 'react'
import type { LibraryCluster } from '../../lib/endpoints'
import { label, mono } from '../../tokens'
import { EpisodeRow } from './EpisodeRow'
import './library.css'

/** 归簇置信度低于该值时标「低置信」，提醒用户分组可能不准 */
export const LOW_CONFIDENCE_THRESHOLD = 0.7

export interface ClusterCardProps {
  cluster: LibraryCluster
  onPlay: (fileId: string) => void
  /** 正在播放的 fileId（高亮对应行）；无播放时传 null */
  activeFileId: string | null
  /** play 请求在途的 fileId；对应行按钮禁用 */
  pendingFileId: string | null
}

/**
 * 一个作品簇的区块卡：标题 + 季/置信度徽标 + 集数，卡内按 groups 分节列出文件行。
 */
export function ClusterCard({ cluster, onPlay, activeFileId, pendingFileId }: ClusterCardProps) {
  const headingId = useId()
  const { title, season, confidence, episodeCount, groups } = cluster
  const isLowConfidence = confidence < LOW_CONFIDENCE_THRESHOLD

  return (
    <article className="panel cluster" aria-labelledby={headingId}>
      <header className="cluster-head">
        <h3 id={headingId} className="cluster-title">
          {title}
        </h3>
        {season !== null && <span className="badge badge--accent">第{season}季</span>}
        {isLowConfidence && (
          <span className="badge badge--warn" title={`归组置信度 ${confidence.toFixed(2)}，分组可能不准`}>
            低置信
          </span>
        )}
        <span className="cluster-count" style={mono}>
          {episodeCount} 集
        </span>
      </header>

      {groups.map((group) => (
        <section key={group.groupKey} className="cluster-group">
          {shouldShowGroupLabel(group.label, title, groups.length) && (
            <h4 className="group-label" style={label}>
              {group.label}
            </h4>
          )}
          <ul className="ep-list">
            {group.items.map((item) => (
              <EpisodeRow
                key={item.fileId}
                item={item}
                onPlay={onPlay}
                isActive={item.fileId === activeFileId}
                isPending={item.fileId === pendingFileId}
              />
            ))}
          </ul>
        </section>
      ))}
    </article>
  )
}

/** 只有一个分组且标签与作品同名（或为空）时不重复展示分组标题 */
function shouldShowGroupLabel(groupLabel: string, clusterTitle: string, groupCount: number): boolean {
  if (groupLabel === '') return false
  if (groupCount > 1) return true
  return groupLabel !== clusterTitle
}
