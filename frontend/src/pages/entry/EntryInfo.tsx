import { useId, useState } from 'react'
import type { ReactNode } from 'react'
import { RouteLink } from '../../components/catalog/RouteLink'
import { DiscoverCard } from '../../components/media/DiscoverCard'
import { FORMAT_LABELS } from '../../components/media/media-format'
import type { MediaSummary } from '../../components/media/types'
import '../../components/catalog/catalog.css'

const SOURCES: Record<string, string> = { ORIGINAL: '原创', MANGA: '漫画', LIGHT_NOVEL: '轻小说', NOVEL: '小说', VIDEO_GAME: '游戏', VISUAL_NOVEL: '视觉小说', WEB_NOVEL: '网络小说', OTHER: '其他' }
const RELATIONS: Record<string, string> = { PREQUEL: '前作', SEQUEL: '续作', SIDE_STORY: '番外', ALTERNATIVE: '其他版本', SUMMARY: '总集篇', PARENT: '本篇', OTHER: '相关作品', SPIN_OFF: '衍生作品' }
/** 简介超过这么多字才折叠：短简介配一个「展开」只是噪音 */
const DESCRIPTION_FOLD = 160

/** 作品信息（目录数据）：简介、事实表、类型标签 */
export function EntryInfo({ media }: { media: MediaSummary }) {
  const headingId = useId()
  const descriptionId = useId()
  const [expanded, setExpanded] = useState(false)
  const description = media.description?.trim() ?? ''
  const foldable = description.length > DESCRIPTION_FOLD
  const facts: [string, string | number | undefined | null][] = [
    ['类型', FORMAT_LABELS[media.format ?? ''] || media.format],
    ['原作', SOURCES[media.source ?? ''] || media.source],
    ['集数', media.episodes],
    ['单集时长', media.duration ? `${media.duration} 分钟` : undefined],
    ['首播日期', media.startDate],
    ['制作公司', media.studios?.join(' / ')],
    ['原名', media.titleNative],
  ]
  return (
    <section className="work-section" aria-labelledby={headingId}>
      <h2 id={headingId} className="work-section-title">作品信息</h2>
      <div className="entry-info">
        <div className="entry-description">
          <p id={descriptionId} className={foldable && !expanded ? 'entry-description-text entry-description-text--folded' : 'entry-description-text'}>{description || '暂无简介。'}</p>
          {foldable && (
            <button type="button" className="link entry-description-toggle" aria-expanded={expanded} aria-controls={descriptionId} onClick={() => setExpanded((v) => !v)}>
              {expanded ? '收起简介' : '展开简介'}
            </button>
          )}
          {media.genres.length > 0 && (
            <ul className="entry-tags" aria-label="类型">
              {media.genres.map((genre) => <li key={genre}><RouteLink to="/" search={{ genre }} className="entry-tag">{genre}</RouteLink></li>)}
            </ul>
          )}
        </div>
        <dl className="entry-facts">
          {facts.map(([label, value]) => (value !== undefined && value !== null && value !== '' ? <div key={label}><dt>{label}</dt><dd>{value}</dd></div> : null))}
        </dl>
      </div>
    </section>
  )
}

/** 角色、关联作品、相关推荐。作品卡与发现页同一种（DiscoverCard）：点进作品页，宽屏悬停浮出播放与磁力 */
export function EntryRelations({ media }: { media: MediaSummary }) {
  const characters = media.characters ?? []
  const relations = media.relations ?? []
  const recommendations = media.recommendations ?? []
  return (
    <>
      {characters.length > 0 && (
        <RelatedSection title="角色">
          <ul className="entry-characters">
            {characters.map((character, index) => (
              <li key={`${character.name}-${index}`}>
                {character.image ? <img src={character.image} alt="" loading="lazy" /> : <span className="entry-character-blank" aria-hidden="true">流</span>}
                <span><strong>{character.name}</strong><small>{character.role === 'MAIN' ? '主角' : '配角'}{character.actor ? ` · ${character.actor}` : ''}</small></span>
              </li>
            ))}
          </ul>
        </RelatedSection>
      )}
      {relations.length > 0 && (
        <RelatedSection title="关联作品">
          <ul className="catalog-grid">{relations.slice(0, 6).map((relation) => <DiscoverCard key={`${relation.type}-${relation.media.id}`} media={relation.media} badge={RELATIONS[relation.type] ?? relation.type} />)}</ul>
        </RelatedSection>
      )}
      {recommendations.length > 0 && (
        <RelatedSection title="相关推荐">
          <ul className="catalog-grid">{recommendations.slice(0, 12).map((item) => <DiscoverCard key={item.id} media={item} />)}</ul>
        </RelatedSection>
      )}
    </>
  )
}

function RelatedSection({ title, children }: { title: string; children: ReactNode }) {
  const headingId = useId()
  return (
    <section className="work-section" aria-labelledby={headingId}>
      <h2 id={headingId} className="work-section-title">{title}</h2>
      {children}
    </section>
  )
}
