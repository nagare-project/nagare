import { Icon } from '../ui/Icon'
import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import type { LibraryCluster } from '../../lib/endpoints'
import { clusterDisplayTitle, needsConfirmation } from '../../lib/associations'

/** 本地作品卡：3:4 海报与缩放交互，点击进入真实文件的剧集列表。 */
export function PosterCard({ cluster }: { cluster: LibraryCluster }) {
  // 记下的是哪个地址加载失败：改了对应作品之后封面换了地址，旧的失败不该把新图也藏起来
  const [failedSrc, setFailedSrc] = useState<string>()
  const { clusterKey, season, episodeCount, confidence, cover } = cluster
  const title = clusterDisplayTitle(cluster)
  const hasCover = cover !== undefined && failedSrc !== cover

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
            <img src={cover} alt="" loading="lazy" onError={() => setFailedSrc(cover)} />
          ) : (
            <span className="poster-art-blank" aria-hidden="true">
              流
            </span>
          )}
          <span className="poster-hover-play"><Icon name="play" size={42} /></span>
          <span className="poster-episodes"><Icon name="folder" size={13} />{episodeCount} 集</span>
          {needsConfirmation(cluster) && (
            <span
              className="badge badge--warn poster-flag"
              title={`归组置信度 ${confidence.toFixed(2)}，对应的作品可能认错了，点进去确认`}
            >
              待确认
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
