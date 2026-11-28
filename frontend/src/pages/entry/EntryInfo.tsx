import { useId, useState } from 'react'
import type { ReactNode } from 'react'
import { RouteLink } from '../../components/catalog/RouteLink'
import { useCollection } from '../../components/media/CollectionContext'
import { genreFilter } from '../../components/media/genres'
import { FORMAT_LABELS } from '../../components/media/media-format'
import { MediaArtwork } from '../../components/media/MediaArtwork'
import { MediaEntryLink } from '../../components/media/MediaEntryLink'
import { MediaListEditor } from '../../components/media/MediaListEditor'
import { cardMeta } from '../../components/media/posterCardParts'
import type { MediaSummary } from '../../components/media/types'
import { useArtworkImage } from '../../components/media/useArtworkImage'
import { Icon } from '../../components/ui/Icon'
import { posterToneStyle } from '../../lib/posterTones'
import './entry-info.css'

const SOURCES: Record<string, string> = {
  ORIGINAL: '原创',
  MANGA: '漫画',
  LIGHT_NOVEL: '轻小说',
  NOVEL: '小说',
  VIDEO_GAME: '游戏',
  VISUAL_NOVEL: '视觉小说',
  WEB_NOVEL: '网络小说',
  OTHER: '其他',
}

const RELATIONS: Record<string, string> = {
  PREQUEL: '前作',
  SEQUEL: '续作',
  SIDE_STORY: '番外',
  ALTERNATIVE: '其他版本',
  SUMMARY: '总集篇',
  PARENT: '本篇',
  OTHER: '相关作品',
  SPIN_OFF: '衍生作品',
}

const STATUS_LABELS: Record<string, string> = {
  RELEASING: '放送中',
  NOT_YET_RELEASED: '尚未播出',
  FINISHED: '已完结',
  CANCELLED: '已取消',
  HIATUS: '暂停放送',
}

/** 简介超过这么多字才折叠：短简介配一个「展开」只是噪音 */
const DESCRIPTION_FOLD = 160

/** 作品信息（目录数据）：无框双栏排版——左简介与类型标签，右发丝线资料表 */
export function EntryInfo({ media }: { media: MediaSummary }) {
  const headingId = useId()
  const descriptionId = useId()
  const [expanded, setExpanded] = useState(false)
  const description = media.description?.trim() ?? ''
  const foldable = description.length > DESCRIPTION_FOLD
  const facts: { label: string; value: string | undefined | null; lang?: string }[] = [
    { label: '类型', value: FORMAT_LABELS[media.format ?? ''] || media.format },
    { label: '原作', value: SOURCES[media.source ?? ''] || media.source },
    { label: '集数', value: media.episodes ? `${media.episodes} 集` : undefined },
    { label: '单集时长', value: media.duration ? `${media.duration} 分钟` : undefined },
    { label: '首播日期', value: media.startDate },
    { label: '制作公司', value: media.studios?.length ? media.studios.join(' / ') : undefined },
    { label: '原名', value: media.titleNative, lang: 'ja' },
  ]
  const visibleFacts = facts.filter((item): item is { label: string; value: string; lang?: string } => Boolean(item.value))

  return (
    <section className="work-section" aria-labelledby={headingId}>
      <h2 id={headingId} className="work-section-title">作品信息</h2>
      <div className="entry-info">
        <div className="entry-description">
          <p
            id={descriptionId}
            className={foldable && !expanded ? 'entry-description-text entry-description-text--folded' : 'entry-description-text'}
          >
            {description || '暂无简介。'}
          </p>
          {foldable && (
            <button
              type="button"
              className="link entry-description-toggle"
              aria-expanded={expanded}
              aria-controls={descriptionId}
              onClick={() => setExpanded((v) => !v)}
            >
              {expanded ? '收起简介' : '展开简介'}
            </button>
          )}
          {media.genres.length > 0 && (
            <ul className="entry-tags" aria-label="类型">
              {media.genres.map((genre) => (
                <li key={genre}>
                  <RouteLink to="/seasonal" search={{ genre: genreFilter(genre) }} className="entry-tag">
                    {genre}
                  </RouteLink>
                </li>
              ))}
            </ul>
          )}
        </div>
        {visibleFacts.length > 0 && (
          <dl className="entry-facts">
            {visibleFacts.map(({ label, value, lang }) => (
              <div key={label}>
                <dt>{label}</dt>
                <dd lang={lang} className={lang === 'ja' ? 'entry-facts-native' : undefined}>
                  {value}
                </dd>
              </div>
            ))}
          </dl>
        )}
      </div>
    </section>
  )
}

/** 角色（无框镜像双侧竖图行）、关联作品（横向关系条卡）、相关推荐（无框影院海报卡） */
export function EntryRelations({ media }: { media: MediaSummary }) {
  const characters = media.characters ?? []
  const relations = (media.relations ?? []).slice(0, 6)
  const recommendations = (media.recommendations ?? []).slice(0, 12)

  return (
    <>
      {characters.length > 0 && (
        <RelatedSection title="角色" count={characters.length}>
          <ul className="entry-characters">
            {characters.map((character, index) => (
              <li key={`${character.name}-${index}`} className="person-row">
                <div className="person-side">
                  <PersonPortrait src={character.image} fallback={character.name} variant="char" />
                  <div className="person-text">
                    <div className="person-role person-role--char">{character.role === 'MAIN' ? '主角' : '配角'}</div>
                    <div className="person-name">{character.name}</div>
                  </div>
                </div>
                {character.actor && (
                  <div className="person-side person-side--va">
                    <PersonPortrait src={character.actorImage} fallback={character.actor} variant="va" />
                    <div className="person-text">
                      <div className="person-role">日语</div>
                      <div className="person-name" lang="ja">{character.actor}</div>
                    </div>
                  </div>
                )}
              </li>
            ))}
          </ul>
        </RelatedSection>
      )}
      {relations.length > 0 && (
        <RelatedSection title="关联作品" count={relations.length}>
          <ul className="relations-grid">
            {relations.map((relation) => (
              <RelationCard key={`${relation.type}-${relation.media.id}`} type={relation.type} media={relation.media} />
            ))}
          </ul>
        </RelatedSection>
      )}
      {recommendations.length > 0 && (
        <RelatedSection title="相关推荐">
          <ul className="recs-grid">
            {recommendations.map((item) => (
              <RecommendationCard key={item.id} item={item} />
            ))}
          </ul>
        </RelatedSection>
      )}
    </>
  )
}

function relationMeta(media: MediaSummary): string {
  const parts: string[] = []
  const format = media.format ? FORMAT_LABELS[media.format] ?? media.format : undefined
  if (format) parts.push(format)
  if (media.episodes && media.format !== 'MOVIE') parts.push(`${media.episodes} 集`)
  if (media.status && STATUS_LABELS[media.status]) parts.push(STATUS_LABELS[media.status]!)
  else if (media.year) parts.push(String(media.year))
  return parts.join(' · ')
}

function RelationCard({ type, media }: { type: string; media: MediaSummary }) {
  const meta = relationMeta(media)
  return (
    <li>
      <MediaEntryLink id={media.id} className="relation-card" style={posterToneStyle(media.posterTones)}>
        <span className="relation-thumb" aria-hidden="true">
          <MediaArtwork src={media.cover} title={media.title} reveal />
        </span>
        <span className="relation-body">
          <span className="relation-kind">{RELATIONS[type] ?? type}</span>
          <span className="relation-title">{media.title}</span>
          {meta && <span className="relation-meta">{meta}</span>}
        </span>
      </MediaEntryLink>
    </li>
  )
}

function RecommendationCard({ item }: { item: MediaSummary }) {
  const collection = useCollection()
  const entry = collection.entries.find((e) => e.anilistId === item.id)
  const media = entry ? { ...item, watched: entry.currentEpisode ?? item.watched } : item
  const meta = cardMeta(media)
  const scoreText = item.score && item.score > 0 ? (item.score / 10).toFixed(1) : null

  return (
    <li className="rec-card" style={posterToneStyle(item.posterTones)}>
      <div className="rec-cover-wrap">
        <MediaEntryLink id={item.id} className="rec-cover" tabIndex={-1} aria-hidden="true">
          <MediaArtwork src={item.cover} title={item.title} reveal />
          {scoreText && <span className="rec-score">{scoreText}</span>}
        </MediaEntryLink>
        <MediaListEditor
          media={media}
          trigger={(props) => (
            <button
              {...props}
              type="button"
              className="rec-add"
              data-follow={entry ? 'on' : 'off'}
              aria-label={entry ? `编辑收藏 ${item.title}` : `加入追番 ${item.title}`}
            >
              <Icon name={entry ? 'check' : 'plus'} size={15} />
            </button>
          )}
        />
      </div>
      <MediaEntryLink id={item.id} className="rec-title">{item.title}</MediaEntryLink>
      {meta && <span className="rec-meta">{meta}</span>}
      {scoreText && <span className="visually-hidden">评分 {scoreText}</span>}
    </li>
  )
}

/**
 * 角色与声优竖图肖像（56×76）：离视口不远就切 eager，避免 WebKit 滚动到角色区才开始加载；
 * 缺图或加载失败时显示首字占位框。
 */
function PersonPortrait({ src, fallback, variant }: { src?: string; fallback: string; variant: 'char' | 'va' }) {
  const [failedSrc, setFailedSrc] = useState<string>()
  const art = useArtworkImage(src)
  const monogram = fallback.trim().slice(0, 1) || '流'
  if (!src || src === failedSrc) {
    return (
      <span
        className={variant === 'va' ? 'person-portrait person-portrait--va' : 'person-portrait person-portrait--blank'}
        aria-hidden="true"
      >
        {monogram}
      </span>
    )
  }
  return (
    <span className="person-portrait">
      <img
        ref={art.ref}
        src={src}
        alt=""
        loading={art.loading}
        onLoad={art.onLoad}
        onError={() => setFailedSrc(src)}
      />
    </span>
  )
}

function RelatedSection({ title, count, children }: { title: string; count?: number; children: ReactNode }) {
  const headingId = useId()
  return (
    <section className="work-section" aria-labelledby={headingId}>
      <h2 className="work-section-title">
        <span id={headingId}>{title}</span>
        {count !== undefined && <span className="work-section-count">{count}</span>}
      </h2>
      {children}
    </section>
  )
}
