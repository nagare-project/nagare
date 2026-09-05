import { Icon } from '../ui/Icon'
import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import type { LibraryCluster } from '../../lib/endpoints'

/** 归簇置信度低于该值时标「低置信」，提醒用户分组可能不准 */
export const LOW_CONFIDENCE_THRESHOLD = 0.7

/** 本地作品卡：3:4 海报与缩放交互，点击进入真实文件的剧集列表。 */
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
          <span className="poster-hover-play"><Icon name="play" size={42} /></span>
          <span className="poster-episodes"><Icon name="folder" size={13} />{episodeCount} 集</span>
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
