import type { CSSProperties } from 'react'
import { cardToneVars } from '../../lib/home/tone'
import type { YearCard } from '../../lib/home/viewModels'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { CoverImage } from './CoverImage'
import { SectionHeader } from './SectionHeader'
import cards from './cards.module.css'
import styles from './YearTopList.module.css'

// 年度榜：本年度 AniList 评分前十，紧凑的名次列表 —— 名次、小图、标题、季度、评分。
// 行与行之间一根发丝线，不画框（DESIGN.md「拆框」：边框只给本身就是对象的东西）。

interface YearTopListProps {
  items: YearCard[]
  year: number
}

export function YearTopList({ items, year }: YearTopListProps) {
  if (items.length === 0) return null
  return (
    <section className={styles.section} aria-labelledby="home-year">
      <SectionHeader id="home-year" title={`${year} 年度榜`} note="按 AniList 评分" phoneHides={['note']} />
      <ol className={styles.list}>
        {items.map((it) => (
          <li key={it.id} className={styles.item}>
            <MediaEntryLink id={it.id} className={styles.row} style={cardToneVars(it.hue) as unknown as CSSProperties}>
              <span className={`${cards.mono} ${styles.rank}`}>{it.rank}</span>
              <CoverImage src={it.cover} className={styles.thumb} />
              <span className={`${cards.clamp1} ${styles.title}`}>{it.title}</span>
              {it.season ? <span className={styles.season}>{it.season}</span> : null}
              <span className={`${cards.mono} ${styles.score}`}>{it.score ?? ''}</span>
            </MediaEntryLink>
          </li>
        ))}
      </ol>
    </section>
  )
}
