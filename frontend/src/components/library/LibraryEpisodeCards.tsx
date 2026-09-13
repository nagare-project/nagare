import { EpisodeArtwork } from '../media/EpisodeArtwork'
import type { EpisodeMetadata } from '../media/types'
import { Link } from '@tanstack/react-router'
import type { LibraryCluster, LibraryItem } from '../../lib/endpoints'
import { formatBytes, formatEpisode, progressPercent } from '../../lib/format'
import { Icon } from '../ui/Icon'
import './episode-cards.css'

interface Props {
  cluster: LibraryCluster
  next?: LibraryItem
  totalEpisodes?: number | null
  titles?: EpisodeMetadata[]
  banner?: string
  cover?: string
  pending: boolean
  activeFileId?: string
  onPlay: (item: LibraryItem) => void
}

/** File-backed episode cards. Number artwork is used until real episode stills are available. */
export function LibraryEpisodeCards({ cluster, next, totalEpisodes, titles, banner, cover, pending, activeFileId, onPlay }: Props) {
  const metadata = new Map(titles?.map(item => [item.episode, item]))
  const titleByEpisode = new Map(titles?.map(item => [item.episode, item.title]))
  const main = cluster.groups.flatMap(group => group.items)
    .filter(item => item.kind === 'main' || item.kind === 'movie' || item.kind === '')
    .sort((a, b) => (a.episode ?? Infinity) - (b.episode ?? Infinity))
  const nextIndex = next ? main.findIndex(item => item.fileId === next.fileId) : -1
  const featured = nextIndex >= 0 ? main.slice(nextIndex, nextIndex + 2) : []
  const title = (item: LibraryItem) => (item.episode !== null ? titleByEpisode.get(item.episode) : undefined) || item.fileName
  const episode = (item: LibraryItem) => item.episode === null ? '本地视频' : `第 ${formatEpisode(item.episode)} 集`
  return <div className="library-episodes">
    {!!featured.length && <div className="episode-featured" aria-label="接下来观看">
      {featured.map((item, index) => <button type="button" className="episode-feature" key={item.fileId} disabled={pending} onClick={() => onPlay(item)}>
        <span className="episode-feature-art" aria-hidden="true"><EpisodeArtwork image={metadata.get(item.episode ?? -1)?.image} banner={banner} cover={cover ?? cluster.cover} /><span className="episode-feature-play"><Icon name="play" size={30} /></span></span>
        <span className="episode-feature-title">{title(item)}</span>
        <span className="episode-feature-meta"><strong>{episode(item)}{totalEpisodes != null && totalEpisodes > 0 && <span> / {totalEpisodes}</span>}</strong><span>{index === 0 && item.progress?.positionSec && !item.progress.completed ? '继续观看' : '播放'}<Icon name="play" size={14} /></span></span>
      </button>)}
    </div>}
    {cluster.groups.map(group => <section className="episode-card-group" key={group.groupKey}>
      <h3>{group.label && group.label !== cluster.title ? group.label : '剧集'}<span>{group.items.length}</span></h3>
      <ul className="episode-card-grid">{group.items.map(item => <li key={item.fileId} className={item.fileId === activeFileId ? 'episode-card episode-card--active' : 'episode-card'}>
        <button type="button" className="episode-card-action" disabled={pending} aria-label={`播放 ${item.fileName}`} onClick={() => onPlay(item)}>
          <span className="episode-card-art" aria-hidden="true"><EpisodeArtwork image={metadata.get(item.episode ?? -1)?.image} banner={banner} cover={cover ?? cluster.cover} /></span>
          <span className="episode-card-copy"><span className="episode-card-meta">{episode(item)}<span>{item.resolution}</span></span><strong title={title(item)}>{title(item)}</strong>{metadata.get(item.episode ?? -1)?.description && <span className="episode-card-description">{metadata.get(item.episode ?? -1)?.description}</span>}{title(item) !== item.fileName && <small title={item.fileName}>{item.fileName}</small>}<span className="episode-card-meta">{formatBytes(item.sizeBytes)}<span>{item.fileId === activeFileId ? '正在播放' : item.progress?.completed ? '已看完' : item.progress?.positionSec ? `已观看 ${progressPercent(item.progress.positionSec, item.progress.durationSec)}%` : '未观看'}</span></span></span>
          <Icon name={item.progress?.completed ? 'check' : 'play'} size={18} />
        </button>
        {item.stream && <Link to="/watch/$fileId" params={{ fileId: item.fileId }} className="episode-card-browser link" aria-label={`在浏览器里播放 ${episode(item)}`}>在浏览器里播放<Icon name="right" size={14} /></Link>}
      </li>)}</ul>
    </section>)}
  </div>
}
