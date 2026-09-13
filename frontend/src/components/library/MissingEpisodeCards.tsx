import { useRef, useState } from 'react'
import type { LibraryCluster } from '../../lib/endpoints'
import { Icon } from '../ui/Icon'
import { EpisodeArtwork } from '../media/EpisodeArtwork'
import { episodeHasAired } from '../media/episode-metadata'
import type { EpisodeMetadata, MediaSummary } from '../media/types'
import './episode-cards.css'

/** 资料卡与可播放文件分开，只有已关联文件才算已入库。 */
export function MissingEpisodeCards({ media, cluster, unassociated = false }: { media: MediaSummary; cluster?: LibraryCluster; unassociated?: boolean }) {
  const localEpisodes = new Set(cluster?.groups.flatMap(group => group.items)
    .filter(item => ['main', 'movie', ''].includes(item.kind)).map(item => item.episode))
  const episodes = (media.episodeTitles ?? []).filter(episode => !localEpisodes.has(episode.episode) && episodeHasAired(episode, media))
    .sort((a, b) => a.episode - b.episode)
  const dialog = useRef<HTMLDialogElement>(null)
  const [details, setDetails] = useState<EpisodeMetadata>()
  if (!episodes.length && cluster) return null
  return <section className="missing-episodes" aria-label={unassociated ? '剧集资料' : '未入库剧集'}>
    <div className="missing-episodes-heading"><h3><Icon name="lists" size={22} />{unassociated ? '剧集资料' : '未入库剧集'}</h3>{!cluster && <a href="/" className="link">添加本地文件夹<Icon name="right" size={14} /></a>}</div>
    <p className="missing-episodes-hint">{unassociated ? '选择并关联本地作品后，可查看剧集的入库情况。' : episodes.length ? '以下剧集尚未添加到本地媒体库：' : '暂无已播出的剧集资料。'}</p>
    <ul className="episode-card-grid missing-episode-grid">{episodes.map(episode => <li className="missing-episode" key={episode.episode}>
      <span className="episode-card-art"><EpisodeArtwork image={episode.image} banner={media.banner} cover={media.cover} /></span>
      <div className="missing-episode-copy"><span>第 {episode.episode} 集</span><strong>{episode.title || `第 ${episode.episode} 集`}</strong>{episode.description && <p>{episode.description}</p>}{episode.airDate && <time dateTime={episode.airDate}><Icon name="calendar" size={13} />播出于 {episode.airDate.replaceAll('-', '/')}</time>}</div>
      <button type="button" className="icon-button missing-episode-info" aria-label={`第 ${episode.episode} 集资料`} onClick={() => { setDetails(episode); dialog.current?.showModal() }}><Icon name="info" size={15} /></button>
    </li>)}</ul>
    <dialog ref={dialog} className="media-description-dialog" aria-label="剧集资料" onClick={event => { if (event.target === dialog.current) dialog.current.close() }}>
      <button type="button" className="icon-button" aria-label="关闭剧集资料" onClick={() => dialog.current?.close()}><Icon name="close" /></button>
      {details && <><h2>第 {details.episode} 集 · {details.title}</h2><div className="episode-info-art"><EpisodeArtwork image={details.image} banner={media.banner} cover={media.cover} /></div><p>{details.description || '暂无本集简介。'}</p>{details.airDate && <p>播出日期：{details.airDate}</p>}</>}
    </dialog>
  </section>
}
