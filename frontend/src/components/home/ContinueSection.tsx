import { useMemo, type CSSProperties } from 'react'
import { RouteLink } from '../catalog/RouteLink'
import { SEASON_LABEL } from '../catalog/season'
import { useCollection } from '../media/CollectionContext'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { Icon } from '../ui/Icon'
import type { CollectionEntry } from '../../lib/endpoints'
import { cardToneVars } from '../../lib/home/tone'
import { entryStatus } from '../../lib/media'
import { CoverImage } from './CoverImage'
import { ChevronIcon } from './icons'
import { SectionHeader } from './SectionHeader'
import cards from './cards.module.css'
import section from './section.module.css'
import styles from './ContinueSection.module.css'

function relativeSeen(sec?: number): string | null {
  if (!sec) return null
  const delta = Math.max(0, Math.floor(Date.now() / 1000) - sec)
  if (delta < 60) return '上次观看 刚刚'
  const min = Math.floor(delta / 60)
  if (min < 60) return `上次观看 ${min} 分钟前`
  const h = Math.floor(min / 60)
  if (h < 24) return `上次观看 ${h} 小时前`
  const d = Math.floor(h / 24)
  if (d < 30) return `上次观看 ${d} 天前`
  return `上次观看 ${Math.floor(d / 30)} 个月前`
}

function seasonText(entry: CollectionEntry): string | null {
  if (!entry.media.year) return null
  const s = entry.media.season ? SEASON_LABEL[entry.media.season as keyof typeof SEASON_LABEL] : undefined
  return s ? `${entry.media.year} ${s}季` : `${entry.media.year}`
}

function entryHue(entry: CollectionEntry): number | null {
  return entry.media.hue ?? entry.media.posterTones?.hue ?? null
}

function ProgressBar({ current, total }: { current: number; total: number | null }) {
  if (!total) return null
  const pct = Math.min(100, (current / total) * 100)
  return (
    <span className={styles.track} aria-hidden="true">
      <span className={styles.fill} style={{ width: `${pct}%` }} />
    </span>
  )
}

/**
 * 发现页「继续看」板块（1:1 对齐网站 ContinueWatching）：
 * 登录且有在追作品时显示在横幅正下方：1 张 2fr 横幅主卡 + 最多 2 张 1fr 紧凑卡，右上角链接到 /lists。
 */
export function ContinueSection() {
  const collection = useCollection()
  const watching = useMemo(() => {
    if (!collection.loggedIn) return []
    return collection.entries
      .filter((e) => entryStatus(e) === 'watching')
      .sort((a, b) => (b.lastWatchedAt ?? 0) - (a.lastWatchedAt ?? 0))
      .slice(0, 3)
  }, [collection.loggedIn, collection.entries])

  if (watching.length === 0) return null
  const [lead, ...rest] = watching
  const leadTotal = lead.media.episodes
  const leadNext = leadTotal ? Math.min(leadTotal, lead.currentEpisode + 1) : lead.currentEpisode + 1
  const leadSeen = relativeSeen(lead.lastWatchedAt)
  const leadSeason = seasonText(lead)
  const leadLive = lead.media.status === 'RELEASING'

  return (
    <section className={section.section} aria-labelledby="home-continue">
      <SectionHeader
        id="home-continue"
        title="继续看"
        action={
          <RouteLink to="/lists" className={section.link}>
            全部在追
            <ChevronIcon className={section.linkIcon} />
          </RouteLink>
        }
      />
      <div className={styles.row}>
        <MediaEntryLink
          id={lead.anilistId}
          className={`${cards.card} ${styles.feature}`}
          style={cardToneVars(entryHue(lead)) as unknown as CSSProperties}
        >
          <span className={styles.featureArt} aria-hidden="true">
            <CoverImage src={lead.media.banner ?? lead.media.cover ?? null} className={styles.featureImg} eager />
          </span>
          <span className={styles.featureShade} aria-hidden="true" />
          <span className={styles.featureBody}>
            <span className={styles.kicker}>
              <span className={styles.dot} data-live={leadLive} aria-hidden="true" />
              {leadLive ? '连载中' : '在追'}
              {leadSeen ? ` · ${leadSeen}` : ''}
            </span>
            <span className={`${cards.clamp1} ${styles.featureTitle}`}>{lead.media.title}</span>
            <span className={styles.featureEp}>
              <span className={styles.featureEpStrong}>
                {lead.currentEpisode > 0 ? `看到第 ${lead.currentEpisode} 集` : '尚未开始'}
              </span>
              {leadSeason ? <span>{leadSeason}</span> : null}
            </span>
            <span className={styles.featureFoot}>
              <span className={styles.featureCta}>
                <Icon name="play" size={14} />
                {lead.currentEpisode > 0 ? `继续看第 ${leadNext} 集` : '开始观看'}
              </span>
              <span className={styles.featureProgress}>
                <ProgressBar current={lead.currentEpisode} total={leadTotal} />
                <span className={`${cards.mono} ${styles.progressText}`}>
                  {leadTotal ? `${lead.currentEpisode}/${leadTotal} 集` : `${lead.currentEpisode} 集`}
                </span>
              </span>
            </span>
          </span>
          <span className={cards.rule} aria-hidden="true" />
        </MediaEntryLink>

        {rest.map((entry) => {
          const total = entry.media.episodes
          const seen = relativeSeen(entry.lastWatchedAt)
          const seasonLine = seasonText(entry)
          const live = entry.media.status === 'RELEASING'
          return (
            <MediaEntryLink
              key={entry.anilistId}
              id={entry.anilistId}
              className={`${cards.card} ${styles.compact}`}
              style={cardToneVars(entryHue(entry)) as unknown as CSSProperties}
            >
              <span className={`${cards.cover} ${styles.compactCover}`}>
                <span className={cards.zoom}>
                  <CoverImage src={entry.media.cover ?? null} className={cards.img} eager />
                </span>
                <span className={cards.rule} aria-hidden="true" />
              </span>
              <span className={styles.compactBody}>
                <span className={styles.chip}>
                  <span className={styles.dot} data-live={live} aria-hidden="true" />
                  {live ? '连载中' : '在追'}
                </span>
                <span className={`${cards.title} ${cards.clamp2} ${styles.compactTitle}`}>{entry.media.title}</span>
                {seen || seasonLine ? <span className={styles.compactLine}>{seen ?? seasonLine}</span> : null}
                <span className={styles.compactFoot}>
                  <span className={styles.compactFootRow}>
                    <span>{entry.currentEpisode > 0 ? `看到第 ${entry.currentEpisode} 集` : '尚未开始'}</span>
                    <span className={cards.mono}>
                      {total ? `${entry.currentEpisode}/${total} 集` : `${entry.currentEpisode} 集`}
                    </span>
                  </span>
                  <ProgressBar current={entry.currentEpisode} total={total} />
                </span>
              </span>
              <span className={`${cards.rule} ${styles.compactRule}`} aria-hidden="true" />
            </MediaEntryLink>
          )
        })}
      </div>
    </section>
  )
}
