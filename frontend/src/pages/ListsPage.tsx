import { useId, useMemo, useState, type CSSProperties } from 'react'
import { RouteLink } from '../components/catalog/RouteLink'
import { SEASON_LABEL } from '../components/catalog/season'
import { CoverImage } from '../components/home/CoverImage'
import hueScope from '../components/home/HomeHueScope.module.css'
import styles from '../components/lists/ListsView.module.css'
import { useCollection } from '../components/media/CollectionContext'
import { MediaEntryLink } from '../components/media/MediaEntryLink'
import { MediaListEditor } from '../components/media/MediaListEditor'
import { Tab, TabCount, TabPanel, Tabs } from '../components/ui'
import { Icon } from '../components/ui/Icon'
import type { CollectionEntry } from '../lib/endpoints'
import { cardToneVars, pageToneVars } from '../lib/home/tone'
import { collectionMedia, COLLECTION_LABELS, entryStatus } from '../lib/media'
import type { CollectionStatus } from '../lib/media'

/** 账号里的四种状态，顺序与账号服务一致 */
const ORDER: readonly CollectionStatus[] = ['watching', 'plan_to_watch', 'completed', 'dropped']

function relativeWatchTime(sec?: number): string | null {
  if (!sec) return null
  const deltaSec = Math.max(0, Math.floor(Date.now() / 1000) - sec)
  if (deltaSec < 60) return '刚刚看过'
  const min = Math.floor(deltaSec / 60)
  if (min < 60) return `${min} 分钟前看过`
  const hours = Math.floor(min / 60)
  if (hours < 24) return `${hours} 小时前看过`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days} 天前看过`
  const months = Math.floor(days / 30)
  return `${months} 个月前看过`
}

function entryHue(entry: CollectionEntry): number | null {
  return entry.media.hue ?? entry.media.posterTones?.hue ?? null
}

function WatchingFeatureCard({ entry, eager }: { entry: CollectionEntry; eager: boolean }) {
  const collection = useCollection()
  const [stepping, setStepping] = useState(false)
  const media = collectionMedia(entry)
  const total = entry.media.episodes
  const current = entry.currentEpisode
  const nextEp = total ? Math.min(total, current + 1) : current + 1
  const canStep = !total || current < total
  const pct = total ? Math.min(100, Math.round((current / total) * 100)) : 0
  const seasonLine = entry.media.year
    ? `${entry.media.year} ${entry.media.season ? `${SEASON_LABEL[entry.media.season as keyof typeof SEASON_LABEL] ?? ''}季` : ''}`.trim()
    : null
  const seen = relativeWatchTime(entry.lastWatchedAt)
  const art = entry.media.banner ?? entry.media.cover ?? null

  async function stepEpisode() {
    if (stepping || !canStep) return
    setStepping(true)
    try {
      const nextStatus: CollectionStatus = total && nextEp >= total ? 'completed' : entryStatus(entry)
      await collection.save(entry.anilistId, {
        status: nextStatus,
        progress: nextEp,
        score: entry.score,
      })
    } finally {
      setStepping(false)
    }
  }

  return (
    <li className={styles.featureCard} style={cardToneVars(entryHue(entry)) as unknown as CSSProperties}>
      {art && (
        <span className={styles.featureArt} aria-hidden="true">
          <CoverImage src={art} className={styles.featureArtImg} eager={eager} />
        </span>
      )}
      <span className={styles.featureShade} aria-hidden="true" />
      <MediaEntryLink id={entry.anilistId} className={styles.featurePoster} aria-hidden="true" tabIndex={-1}>
        <CoverImage src={entry.media.cover ?? null} className={styles.featurePosterImg} eager={eager} />
      </MediaEntryLink>
      <div className={styles.featureBody}>
        <span className={styles.featureKicker}>
          <span className={styles.dot} aria-hidden="true" />
          <span>{entry.media.status === 'RELEASING' ? '连载中' : '在追'}</span>
          {seen ? <span>· {seen}</span> : null}
          {seasonLine ? <span>· {seasonLine}</span> : null}
        </span>
        <MediaEntryLink id={entry.anilistId} className={styles.featureTitle}>
          {entry.media.title}
        </MediaEntryLink>
        <div className={styles.featureEp}>
          <span className={styles.featureEpStrong}>
            {current > 0 ? `看到第 ${current} 集` : '尚未开始'}
          </span>
          {entry.score ? <span>我的评分 {entry.score}</span> : null}
        </div>
        <div className={styles.progressWrap}>
          {total ? (
            <span className={styles.track} aria-hidden="true">
              <span className={styles.fill} style={{ width: `${pct}%` }} />
            </span>
          ) : null}
          <span className={styles.progressText}>
            {total ? `${current}/${total} 集` : `${current} 集`}
          </span>
        </div>
        <div className={styles.featureFoot}>
          <MediaEntryLink id={entry.anilistId} className={styles.continueBtn}>
            <Icon name="play" size={14} />
            <span>{current > 0 ? `继续看第 ${nextEp} 集` : '开始观看'}</span>
          </MediaEntryLink>
          {canStep && (
            <button
              type="button"
              className={styles.stepBtn}
              disabled={stepping}
              aria-label={`「${entry.media.title}」进度 +1 集`}
              onClick={() => void stepEpisode()}
            >
              <Icon name="plus" size={13} />
              <span>{stepping ? '保存中…' : '+1 集'}</span>
            </button>
          )}
          <MediaListEditor
            media={media}
            trigger={(props) => (
              <button
                {...props}
                type="button"
                className={styles.editBtn}
                aria-label={`编辑「${entry.media.title}」的追番记录`}
              >
                <Icon name="edit" size={15} />
              </button>
            )}
          />
        </div>
      </div>
      <span className={styles.rule} aria-hidden="true" />
    </li>
  )
}

function TintedPosterCard({ entry, eager }: { entry: CollectionEntry; eager: boolean }) {
  const media = collectionMedia(entry)
  const total = entry.media.episodes
  const pct = total ? Math.min(100, Math.round((entry.currentEpisode / total) * 100)) : 0
  return (
    <li className={styles.posterCard} style={cardToneVars(entryHue(entry)) as unknown as CSSProperties}>
      <MediaEntryLink id={entry.anilistId} className={styles.posterMedia} aria-hidden="true" tabIndex={-1}>
        <CoverImage src={entry.media.cover ?? null} className={styles.posterImg} eager={eager} />
        {entry.score ? <span className={styles.posterBadge}>★ {entry.score}</span> : null}
        {total && entry.currentEpisode > 0 ? (
          <span className={styles.posterBar} aria-hidden="true">
            <span className={styles.posterBarFill} style={{ width: `${pct}%` }} />
          </span>
        ) : null}
      </MediaEntryLink>
      <div className={styles.posterFoot}>
        <div className={styles.posterMeta}>
          <MediaEntryLink id={entry.anilistId} className={styles.posterTitle}>
            {entry.media.title}
          </MediaEntryLink>
          <span className={styles.posterSub}>
            {total ? `${entry.currentEpisode}/${total} 集` : entry.currentEpisode > 0 ? `第 ${entry.currentEpisode} 集` : '未开始'}
          </span>
        </div>
        <MediaListEditor
          media={media}
          trigger={(props) => (
            <button
              {...props}
              type="button"
              className={styles.posterEdit}
              aria-label={`编辑「${entry.media.title}」的追番记录`}
            >
              <Icon name="edit" size={14} />
            </button>
          )}
        />
      </div>
    </li>
  )
}

/** 我的追番（`/lists`，对齐网站 WatchingList）：四档追番状态、在看横向进度卡（支持 +1 集）、色调页底 */
export function ListsPage() {
  const [status, setStatus] = useState<CollectionStatus>('watching')
  const collection = useCollection()
  const items = useMemo(
    () => collection.entries.filter((entry) => entryStatus(entry) === status),
    [collection.entries, status],
  )
  const pageHue = useMemo(() => {
    for (const entry of items) {
      const h = entryHue(entry)
      if (h !== null) return h
    }
    return null
  }, [items])
  const retry = () => void collection.reload()
  const tabs = useId()

  const content = (
    <div className={styles.body}>
      {collection.error && collection.entries.length > 0 && (
        <p className={`result result--err ${styles.alert}`} role="alert">
          {collection.error}{' '}
          <button type="button" className="link" onClick={retry}>重试</button>
        </p>
      )}
      {collection.loading ? (
        <div className={styles.skeletonGrid} role="status" aria-busy="true">
          <span className="visually-hidden">正在读取追番列表</span>
          {Array.from({ length: 4 }, (_, i) => <div key={i} className={styles.skeletonCard} aria-hidden="true" />)}
        </div>
      ) : collection.error && collection.entries.length === 0 ? (
        <section className={styles.panel}>
          <h2 className={styles.panelTitle}>追番列表读取失败</h2>
          <p className={styles.panelBody}>{collection.error}</p>
          <div className={styles.panelActions}>
            <button type="button" className={styles.cta} onClick={retry}>重试</button>
          </div>
        </section>
      ) : !collection.loggedIn ? (
        <section className={styles.panel}>
          <h2 className={styles.panelTitle}>登录后显示你的追番</h2>
          <p className={styles.panelBody}>登录账号后，这里会列出在看、想看、看完和弃番的作品，看完的集数也会自动同步。</p>
          <div className={styles.panelActions}>
            <RouteLink to="/settings#account" className={styles.cta}>去登录</RouteLink>
          </div>
        </section>
      ) : items.length === 0 ? (
        <section className={styles.panel}>
          <h2 className={styles.panelTitle}>「{COLLECTION_LABELS[status]}」里还没有作品</h2>
          <p className={styles.panelBody}>在目录卡片或作品详情页点「追番」，就能把想追的新番收进这里。</p>
          <div className={styles.panelActions}>
            <RouteLink to="/seasonal" className={styles.cta}>去看看本季新番</RouteLink>
            <RouteLink to="/" className={styles.cta}>去发现</RouteLink>
          </div>
        </section>
      ) : status === 'watching' ? (
        <ul className={styles.featureGrid}>
          {items.map((entry, idx) => (
            <WatchingFeatureCard key={entry.anilistId} entry={entry} eager={idx < 4} />
          ))}
        </ul>
      ) : (
        <ul className={styles.posterGrid}>
          {items.map((entry, idx) => (
            <TintedPosterCard key={entry.anilistId} entry={entry} eager={idx < 10} />
          ))}
        </ul>
      )}
    </div>
  )

  return (
    <main className={`${hueScope.scope} ${styles.page}`} style={pageToneVars(pageHue) as unknown as CSSProperties}>
      <header className={styles.header}>
        <div className={styles.heading}>
          <span className={styles.eyebrow}>账号同步 · 追番进度</span>
          <h1 className={styles.title}>我的追番</h1>
          {collection.loggedIn && (
            <p className={styles.summary}>
              共记录 {collection.entries.length} 部作品 · 当前「{COLLECTION_LABELS[status]}」{items.length} 部
            </p>
          )}
        </div>
      </header>

      {collection.loggedIn ? (
        <>
          <div className={styles.tabsWrap}>
            <Tabs id={tabs} value={status} onChange={(next) => setStatus(next as CollectionStatus)} label="观看状态">
              {ORDER.map((value) => (
                <Tab key={value} value={value}>
                  {COLLECTION_LABELS[value]}
                  <TabCount n={collection.entries.filter((entry) => entryStatus(entry) === value).length} />
                </Tab>
              ))}
            </Tabs>
          </div>
          <TabPanel tabs={tabs} value={status} hidden={false}>
            {content}
          </TabPanel>
        </>
      ) : (
        content
      )}
    </main>
  )
}

/**
 * 假数据横幅。存在的理由：演示界面时必须一眼看得出哪些数字是真的。
 * 不写这一条，截图发出去别人会当成功能已经做好了。
 */
export function FixtureNotice({ gap, what }: { gap: string; what: string }) {
  return (
    <p className="alert-warn fixture-notice" role="status">
      本页的{what}是<strong>假数据</strong>，用于界面演示，尚未连接你的账号。<span className="visually-hidden">缺口 {gap}</span>
    </p>
  )
}
