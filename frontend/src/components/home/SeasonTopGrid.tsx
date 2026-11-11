import { memo } from 'react'
import type { CSSProperties } from 'react'
import { upcomingAiring, type Airing } from '../../lib/home/heroStatus'
import { weekdayTime } from '../../lib/home/time'
import { cardToneVars } from '../../lib/home/tone'
import type { SeasonCard } from '../../lib/home/viewModels'
import { RouteLink } from '../catalog/RouteLink'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { CoverImage } from './CoverImage'
import { SectionHeader } from './SectionHeader'
import { ChevronIcon, StarIcon } from './icons'
import { useHomeClock } from './useHomeClock'
import cards from './cards.module.css'
import section from './section.module.css'
import styles from './SeasonTopGrid.module.css'

// 本季高分：本季 AniList 评分最高的十二部，一行六张；鼠标停上去或键盘聚焦时，旁边浮出一张详情浮层。
// 浮层是纯 CSS（:hover / :focus-visible），复述作品页上已有的信息，对读屏隐藏；可访问的对象是卡片链接本身。

interface SeasonTopGridProps {
  items: SeasonCard[]
  /** 「2026 秋季」 */
  seasonLabel: string
  /** 季度页的查询参数（本季） */
  seasonSearch: Record<string, unknown>
  /** 这一季一共几部，「本季全部 N 部」 */
  total: number
}

export const SeasonTopGrid = memo(function SeasonTopGrid({ items, seasonLabel, seasonSearch, total }: SeasonTopGridProps) {
  if (items.length === 0) return null
  const allLabel = `本季全部 ${total} 部`

  return (
    <section className={section.section} aria-labelledby="home-season-top">
      <SectionHeader
        id="home-season-top"
        title="本季高分"
        note={`${seasonLabel} · 按 AniList 评分`}
        link={total > 0 ? { to: '/seasonal', search: seasonSearch, label: allLabel } : undefined}
        phoneHides={['link']}
      />
      <div className={styles.grid}>
        {items.map((it) => (
          <MediaEntryLink key={it.id} id={it.id} className={`${cards.card} ${styles.item}`} style={cardToneVars(it.hue) as unknown as CSSProperties}>
            <div className={`${cards.cover} ${styles.cover}`}>
              <span className={cards.zoom}>
                <CoverImage src={it.cover} className={cards.img} />
              </span>
              <span className={cards.rule} aria-hidden="true" />
            </div>
            <div className={styles.body}>
              <span className={`${cards.title} ${cards.clamp2} ${styles.title}`}>{it.title}</span>
              <span className={cards.meta}>
                {it.score ? <span className={cards.score}>{it.score}</span> : null}
                <span className={cards.clamp1}>{it.meta}</span>
              </span>
            </div>
            <div className={styles.pop} aria-hidden="true">
              <span className={styles.popStatus}>
                {it.statusLabel}
                {it.airings.length > 0 ? <AiringWhen airings={it.airings} /> : null}
              </span>
              {it.score || it.bangumi ? (
                <span className={styles.popScore}>
                  <StarIcon className={styles.popStar} />
                  {it.score ? <span className={`${cards.mono} ${styles.popScoreValue}`}>{it.score}</span> : null}
                  <span>
                    {it.score ? 'AniList' : ''}
                    {it.score && it.bangumi ? ' · ' : ''}
                    {it.bangumi ? `Bangumi ${it.bangumi}` : ''}
                  </span>
                </span>
              ) : null}
              {it.popMeta ? <span className={styles.popMeta}>{it.popMeta}</span> : null}
              {it.genres.length > 0 ? (
                <span className={styles.popTags}>
                  {it.genres.map((g) => (
                    <span key={g} className={styles.popTag}>
                      {g}
                    </span>
                  ))}
                </span>
              ) : null}
            </div>
          </MediaEntryLink>
        ))}
      </div>
      {total > 0 ? (
        <RouteLink to="/seasonal" search={seasonSearch} className={styles.allMobile}>
          {allLabel}
          <ChevronIcon />
        </RouteLink>
      ) : null}
    </section>
  )
})

/** 浮层里的「 · 周日 19:00 更新第 14 集」：按时钟挑还没播的第一集，那一集播出后换成下一集（近七日里没有了就不说） */
function AiringWhen({ airings }: { airings: Airing[] }) {
  const next = upcomingAiring(airings, useHomeClock())
  if (!next) return null
  return <>{` · ${weekdayTime(next.at)} 更新第 ${next.ep} 集`}</>
}
