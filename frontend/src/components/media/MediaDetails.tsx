import { COLLECTION_LABELS } from '../../lib/media'
import { audienceColor } from './media-format'
import type { ReactNode } from 'react'
import { useRef } from 'react'
import { Icon } from '../ui/Icon'
import { MediaArtwork } from './MediaArtwork'
import { MediaListEditor } from './MediaListEditor'
import { MediaEntryLink } from './MediaEntryLink'
import { DiscoverCard } from './DiscoverCard'
import { useCollection } from './CollectionContext'
import type { MediaSummary } from './types'
import './media-details.css'
import './discover.css'

const STATUS: Record<string, string> = { RELEASING: '放送中', NOT_YET_RELEASED: '尚未播出', FINISHED: '已完结', CANCELLED: '已取消', HIATUS: '暂停放送' }
const RELATIONS: Record<string, string> = { PREQUEL: '前作', SEQUEL: '续作', SIDE_STORY: '番外', ALTERNATIVE: '其他版本', SUMMARY: '总集篇', PARENT: '本篇', OTHER: '相关作品', SPIN_OFF: '衍生作品' }

export function MediaDetails({ media, titleId, page = false, children }: { media: MediaSummary; titleId?: string; page?: boolean; children?: ReactNode }) {
  const collection = useCollection()
  const entry = collection.entries.find(entry => entry.anilistId === media.id)
  const watched = entry?.currentEpisode ?? media.watched
  const subtitle = media.titleEnglish?.toLowerCase() !== media.title.toLowerCase() ? media.titleEnglish : undefined
  const rankings = (media.rankings ?? []).filter(rank => rank.type === 'RATED' && rank.rank <= (rank.year || rank.season ? 5 : 100)).slice(0, 2)
  const description = useRef<HTMLDialogElement>(null)
  const DescriptionTitle = page ? 'h1' : 'h2'
  return <>
    <div className="media-preview-heading">
      <span className="media-preview-art"><MediaArtwork src={media.cover} title={media.title} eager /></span>
      <div className="media-preview-info">
        <DescriptionTitle id={titleId} className="media-detail-title">{media.title}</DescriptionTitle>
        {subtitle && <p className="media-preview-native">{subtitle}</p>}
        <div className="media-preview-meta"><span className="media-preview-progress">{watched}<span>/{media.episodes ?? '—'}</span></span><MediaListEditor media={media} />{page && entry && <span className="media-detail-collection-status">{COLLECTION_LABELS[entry.status]}</span>}
          {(media.year || media.season) && <span className="media-detail-date"><Icon name="calendar" size={20} />{media.startDate?.slice(0, 7) || media.year} {media.season}</span>}
          {media.status && media.status !== 'FINISHED' && <span className="media-detail-status"><Icon name="broadcast" size={18} />{STATUS[media.status] ?? media.status}</span>}
        </div>
        <div className="media-preview-genres">
          {!!media.score && <span className="media-preview-score" style={{ color: audienceColor(media.score) }}><Icon name="heart" size={12} />{media.score / 10}</span>}
          {media.studios?.map(studio => <span className="media-detail-studio" key={studio}>{studio}</span>)}
          {media.genres.map(genre => <a key={genre} href={`/discover?genre=${encodeURIComponent(genre)}`}>{genre}</a>)}
        </div>
        {!!rankings.length && <div className="media-detail-rankings">{rankings.map((rank, index) => <span key={index}><Icon name="star" size={16} />#{rank.rank} {rank.year || '最高评分'} {rank.season ? ({ WINTER: '冬', SPRING: '春', SUMMER: '夏', FALL: '秋' } as Record<string, string>)[rank.season] : ''}</span>)}</div>}
        <button type="button" className="media-detail-description" aria-label="展开完整简介" onClick={() => description.current?.showModal()}>{media.description || '暂无简介。'}</button>
        {children}
        <dialog ref={description} className="media-description-dialog" aria-label={`${media.title} 简介`} onClose={event => event.stopPropagation()} onClick={event => { if (event.target === description.current) description.current.close() }}>
          <button type="button" className="icon-button" aria-label="关闭简介" onClick={() => description.current?.close()}><Icon name="close" /></button><h2>{media.title}</h2><p>{media.description || '暂无简介。'}</p>
        </dialog>
      </div>
    </div>
  </>
}

export function MediaRelations({ media, preview = false }: { media: MediaSummary; preview?: boolean }) {
  return <div className={preview ? 'media-relations media-relations--preview' : 'media-relations'}>

    {!preview && !!media.characters?.length && <section><h2>角色</h2><ul className="media-character-grid">{media.characters.map((character, index) => <li key={index}>
      <MediaArtwork src={character.image} title={character.name} /><div><strong>{character.name}</strong><span>{character.role === 'MAIN' ? '主角' : '配角'}</span></div>
      {character.actor && <div className="media-character-actor"><strong>{character.actor}</strong><span>日语配音</span></div>}{character.actorImage && <MediaArtwork src={character.actorImage} title={character.actor ?? ''} />}
    </li>)}</ul></section>}
    {!!media.relations?.length && <section><h2>关联作品</h2><ul className="media-detail-grid">{media.relations.slice(0, 4).map(relation => <DiscoverCard key={`${relation.type}-${relation.media.id}`} media={relation.media} badge={RELATIONS[relation.type] ?? relation.type} />)}</ul></section>}
    {!!media.recommendations?.length && <section><h2>相关推荐</h2><ul className="media-detail-grid">{media.recommendations.map(item => <DiscoverCard key={item.id} media={item} />)}</ul></section>}
  </div>
}

export function MediaExternalLinks({ media, preview = false }: { media: MediaSummary; preview?: boolean }) {
  return <>{preview && <MediaEntryLink id={media.id} className="media-preview-link">打开作品页</MediaEntryLink>}<a className="media-preview-link media-anilist-link" href={`https://anilist.co/anime/${media.id}`} target="_blank" rel="noreferrer" aria-label="在 AniList 查看">A<span>L</span></a></>
}
