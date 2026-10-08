import { Icon } from '../ui/Icon'
import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { needsConfirmation } from '../../lib/associations'
import { seriesProgress } from '../../lib/librarySeries'
import type { LibrarySeries } from '../../lib/librarySeries'

/**
 * 本地作品卡：3:4 海报与缩放交互。认出了目录作品的进作品详情页（本地剧集、磁力、在线都在那里），
 * 没认出的、或认出的作品与同作品的其他版本季数对不上的，进媒体库里这个分组的文件列表（在那里确认作品）。
 */
export function PosterCard({ series }: { series: LibrarySeries }) {
  // 记下的是哪个地址加载失败：改了对应作品之后封面换了地址，旧的失败不该把新图也藏起来
  const [failedSrc, setFailedSrc] = useState<string>()
  const { work, ambiguous, title, cover, clusters, episodeCount, season } = series
  const primary = clusters[0]!
  const hasCover = cover !== undefined && failedSrc !== cover
  // 看完了几集：多个版本的同一集只算一次
  const watched = seriesProgress(clusters).completed
  const label = `${title}，共 ${episodeCount} 集${clusters.length > 1 ? `，${clusters.length} 个版本` : ''}${watched > 0 ? `，已看 ${watched} 集` : ''}`

  const art = (
    <>
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
        {clusters.length > 1 && <span className="poster-versions" title="同一部作品在媒体库里有多个版本（不同字幕组或文件夹）">{clusters.length} 个版本</span>}
        {ambiguous ? (
          <span className="badge badge--warn poster-flag" title="和另一个文件夹认成了同一部作品，但两边的季数不一样，多半有一个认错了，点进去确认">
            待确认
          </span>
        ) : work === undefined && needsConfirmation(primary) && (
          <span
            className="badge badge--warn poster-flag"
            title={`归组置信度 ${primary.confidence.toFixed(2)}，还没认出是哪部作品，点进去选择`}
          >
            待确认
          </span>
        )}
      </span>
      <p className="poster-title">{title}</p>
      <p className="poster-meta">
        {season !== null ? `第 ${season} 季 · ` : ''}
        {episodeCount} 集{watched > 0 && ` · 已看 ${watched}`}
      </p>
    </>
  )

  return (
    <li className="poster">
      {work && !ambiguous ? (
        <Link to="/entry" search={{ id: work.anilistId }} className="poster-hit" aria-label={label}>{art}</Link>
      ) : (
        <Link to="/anime/$clusterKey" params={{ clusterKey: primary.clusterKey }} className="poster-hit" aria-label={label}>{art}</Link>
      )}
    </li>
  )
}
