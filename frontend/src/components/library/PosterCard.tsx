import { Link } from '@tanstack/react-router'
import { needsConfirmation } from '../../lib/associations'
import { seriesProgress } from '../../lib/librarySeries'
import type { LibrarySeries } from '../../lib/librarySeries'
import { PosterTile } from './PosterTile'
import type { PosterBadge } from './PosterTile'

/**
 * 媒体库的一张作品卡（DESIGN.md「媒体库 ②」）。认出了目录作品的进作品页（本地剧集、磁力、在线都在那里），
 * 没认出的、或认出的作品与同作品的其他版本季数对不上的，进媒体库里这个分组的页面（在那里认作品）。
 * identifying：后台还在认作品、这个分组还没认出来 —— 灰色占位 +「认作品中…」，认出后原地换成海报。
 */
export function PosterCard({ series, identifying = false }: { series: LibrarySeries; identifying?: boolean }) {
  const { work, ambiguous, title, cover, clusters, episodeCount, season } = series
  const primary = clusters[0]!
  // 看完了几集：多个版本的同一集只算一次
  const watched = seriesProgress(clusters).completed
  const badges: PosterBadge[] = []
  if (ambiguous) {
    badges.push({ text: '待确认', tone: 'warning', hint: '和另一个文件夹认成了同一部作品，但两边的季数不一样，多半有一个认错了，点进去确认' })
  } else if (work === undefined && needsConfirmation(primary)) {
    badges.push({ text: '待确认', tone: 'warning', hint: `归组置信度 ${primary.confidence.toFixed(2)}，还没认出是哪部作品，点进去选择` })
  }
  if (clusters.length > 1) badges.push({ text: `${clusters.length} 个版本`, hint: '同一部作品在媒体库里有多个版本（不同字幕组或文件夹）' })
  const meta = [season !== null ? `第 ${season} 季` : null, `${episodeCount} 集`, watched > 0 ? `已看 ${watched}` : null, identifying ? '认作品中…' : null]
    .filter(Boolean).join(' · ')
  const body = <PosterTile cover={identifying ? undefined : cover} title={title} meta={meta} badges={badges} />

  return (
    <li className={identifying ? 'poster-tile poster-tile--pending' : 'poster-tile'}>
      {work && !ambiguous ? (
        <Link to="/entry" search={{ id: work.anilistId }} className="poster-tile-hit">{body}</Link>
      ) : (
        <Link to="/anime/$clusterKey" params={{ clusterKey: primary.clusterKey }} className="poster-tile-hit">{body}</Link>
      )}
    </li>
  )
}
