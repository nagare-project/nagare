import type { CSSProperties } from 'react'
import { cardToneVars } from '../../lib/home/tone'
import type { TrendCard } from '../../lib/home/viewModels'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { CoverImage } from './CoverImage'
import { SectionHeader } from './SectionHeader'
import { ChevronIcon } from './icons'
import cards from './cards.module.css'
import section from './section.module.css'
import styles from './TrendingRanks.module.css'

// 大家都在追：追番人数最多的作品，大号名次数字用各自作品的颜色。宽屏一行五张，手机上是五行的列表。
// 除了 CSS 悬停，这里没有东西会动。

export function TrendingRanks({ items }: { items: TrendCard[] }) {
  if (items.length === 0) return null
  return (
    <section className={section.section} aria-labelledby="home-trending">
      <SectionHeader id="home-trending" title="大家都在追" note="按追番人数" />
      <ol className={styles.list}>
        {items.map((it) => (
          <li key={it.id} className={styles.item}>
            <MediaEntryLink id={it.id} className={`${cards.card} ${styles.card}`} style={cardToneVars(it.hue) as unknown as CSSProperties}>
              <div className={styles.art}>
                <span className={`${cards.mono} ${styles.rank}`} aria-hidden="true">
                  {it.rank}
                </span>
                <div className={`${cards.cover} ${styles.cover}`}>
                  <span className={cards.zoom}>
                    <CoverImage src={it.cover} className={cards.img} />
                  </span>
                  <span className={cards.rule} aria-hidden="true" />
                </div>
              </div>
              <div className={styles.body}>
                <span className={`${cards.title} ${cards.clamp1} ${styles.title}`}>
                  <span className={section.srOnly}>{`第 ${it.rank} 名 `}</span>
                  {it.title}
                </span>
                <span className={styles.line}>
                  {`${it.watchers} 人在追`}
                  {it.scoreLine ? ` · ${it.scoreLine}` : ''}
                </span>
              </div>
              <ChevronIcon size={16} className={styles.chevron} />
            </MediaEntryLink>
          </li>
        ))}
      </ol>
    </section>
  )
}
