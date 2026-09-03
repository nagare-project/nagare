import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import type { LibraryCluster } from '../../lib/endpoints'

/** 归簇置信度低于该值时标「低置信」，提醒用户分组可能不准 */
export const LOW_CONFIDENCE_THRESHOLD = 0.7

/**
 * 媒体库网格里的一张海报卡。
 *
 * 比例、圆角、hover 幅度都是从运行中的 seanime 量出来的，不是估的：
 * 海报 6/8（3:4）、圆角 .5rem、hover 放大到 1.1、200ms ease-out。
 * 3:4 正是动漫海报的通用比例，用它意味着 animego 给的封面不必裁切。
 *
 * 封面缺失是常态（只有播放过的条目才匹配过、才有图），所以无图版式
 * 不是兜底而是一等公民：它保持同样的比例占位，网格不会因为缺图而参差。
 */
export function PosterCard({ cluster }: { cluster: LibraryCluster }) {
  const [failed, setFailed] = useState(false)
  const { clusterKey, title, season, episodeCount, confidence, cover } = cluster
  const hasCover = cover !== undefined && !failed

  return (
    <li className="poster">
      <Link
        to="/anime/$clusterKey"
        params={{ clusterKey }}
        className="poster-hit"
        aria-label={`${title}，共 ${episodeCount} 集`}
      >
        <span className="poster-art">
          {hasCover ? (
            <img src={cover} alt="" loading="lazy" onError={() => setFailed(true)} />
          ) : (
            <span className="poster-art-blank" aria-hidden="true">
              流
            </span>
          )}
          {confidence < LOW_CONFIDENCE_THRESHOLD && (
            <span
              className="badge badge--warn poster-flag"
              title={`归组置信度 ${confidence.toFixed(2)}，分组可能不准`}
            >
              低置信
            </span>
          )}
        </span>
        <p className="poster-title">{title}</p>
        <p className="poster-meta">
          {season !== null ? `第 ${season} 季 · ` : ''}
          {episodeCount} 集
        </p>
      </Link>
    </li>
  )
}
