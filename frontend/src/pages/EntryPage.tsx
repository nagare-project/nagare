import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { useSearch } from '@tanstack/react-router'
import { MediaPlayButton } from '../components/media/MediaPlayButton'
import type { LocalPrimary } from '../components/media/MediaPlayButton'
import { MediaSourceButton } from '../components/media/MediaSourceButton'
import { MediaTorrentButton } from '../components/media/MediaTorrentButton'
import { MediaListEditor } from '../components/media/MediaListEditor'
import { TrailerPreview } from '../components/media/MediaPreview'
import { useMediaDetails } from '../components/media/useMediaDetails'
import { useEpisodeMetadata } from '../components/media/useEpisodeMetadata'
import { useCollection } from '../components/media/CollectionContext'
import type { MediaSummary } from '../components/media/types'
import { airingDistance, FORMAT_LABELS } from '../components/media/media-format'
import { nextEpisodeAiring } from '../components/media/episode-metadata'
import { Icon } from '../components/ui/Icon'
import { Tabs, Tab } from '../components/ui'
import { WorkHero } from '../components/library/WorkHero'
import { FreshnessNote } from '../components/catalog/FreshnessNote'
import { RouteLink } from '../components/catalog/RouteLink'
import { LoginHint, WorkHeroSkeleton } from '../components/library/WorkStates'
import { LibraryAnimeLink } from '../components/library/LibraryAnimeLink'
import { fetchLibrary } from '../lib/endpoints'
import type { LibraryCluster } from '../lib/endpoints'
import { clusterWork } from '../lib/librarySeries'
import { posterToneStyle } from '../lib/posterTones'
import { EntryInfo, EntryRelations } from './entry/EntryInfo'
import '../components/media/media-preview.css'
import './entry.css'
import './entry/entry-page.css'

const SEASONS: Record<string, string> = { WINTER: '冬', SPRING: '春', SUMMER: '夏', FALL: '秋' }
const STATUS: Record<string, string> = { RELEASING: '放送中', NOT_YET_RELEASED: '尚未播出', FINISHED: '已完结', CANCELLED: '已取消', HIATUS: '暂停放送' }
const PANELS = { local: '本地媒体库', online: '在线播放' } as const
type Panel = keyof typeof PANELS
/** 预告片只认 YouTube 视频 ID 的形状（11 位），别的不往 iframe 里放 */
const YOUTUBE_ID = /^[\w-]{11}$/

/**
 * `/entry?id=` 目录作品页（DESIGN.md「作品页」）：hero（海报、标题、动作行）→ 剧集（本地状态叠在目录资料上；
 * 在线播放是另一个标签）→ 作品信息 → 角色与关联。磁力是 hero 上的「磁力」按钮，打开右侧抽屉。
 */
export function EntryPage() {
  const { id } = useSearch({ from: '/entry' })
  const { media, loading, error, retry } = useMediaDetails(id)
  if (media) return <EntryContent key={id} media={media} />
  return (
    <main className="work-page">
      {loading && <WorkHeroSkeleton label="正在加载作品详情" />}
      {error && (
        <div className="work-body entry-fallback">
          <p className="result result--err" role="alert">{error} <button type="button" className="link" onClick={retry}>重试</button> <RouteLink className="link" to="/">返回发现</RouteLink></p>
          <LocalFallback id={id} />
        </div>
      )}
    </main>
  )
}

function EntryContent({ media }: { media: MediaSummary }) {
  const [panel, setPanel] = useState<Panel>('local')
  const [primary, setPrimary] = useState<LocalPrimary | null>(null)
  const collection = useCollection()
  const entry = collection.entries.find((item) => item.anilistId === media.id)
  const watched = entry?.currentEpisode ?? media.watched
  const episodeMetadata = useEpisodeMetadata(media.id)
  const titles = new Map(media.episodeTitles?.map((ep) => [ep.episode, ep]))
  for (const ep of episodeMetadata.episodes) titles.set(ep.episode, { ...ep, title: titles.get(ep.episode)?.title || ep.title })
  const nextAiring = nextEpisodeAiring(media, episodeMetadata.episodes)
  const playbackMedia = { ...media, watched, episodeTitles: [...titles.values()] }
  const tones = posterToneStyle(media.posterTones)

  useEffect(() => {
    window.scrollTo(0, 0)
    const previous = document.title
    document.title = `${media.title} · nagare`
    return () => { document.title = previous }
  }, [media.id, media.title])

  const meta = [
    FORMAT_LABELS[media.format ?? ''] || media.format,
    media.year ? `${media.year}${media.season ? ` ${SEASONS[media.season] ?? ''}` : ''}` : null,
    media.episodes ? `${media.episodes} 集` : null,
    media.duration ? `${media.duration} 分钟/集` : null,
    media.status ? STATUS[media.status] ?? media.status : null,
    watched > 0 ? `看到第 ${watched} 集` : null,
  ]

  return (
    <main className="work-page entry-page" style={tones}>
      <WorkHero
        cover={media.cover}
        backdrop={media.banner ?? media.cover}
        backdropIsCover={!media.banner}
        toned={tones !== undefined}
        title={media.title}
        romaji={media.titleRomaji}
        native={media.titleNative}
        meta={meta}
        actions={
          <>
            {primary && (
              // 播放的进度与出错说明都画在「本地媒体库」那个标签里：先切过去，否则停在「在线播放」时失败是静默的
              <button type="button" className="hero-btn hero-btn--solid" disabled={primary.busy} onClick={() => { setPanel('local'); primary.play() }}>
                <Icon name="play" size={16} />{primary.busy ? '正在启动…' : primary.label}
              </button>
            )}
            <MediaListEditor media={media} labelled />
            <MediaTorrentButton media={playbackMedia} drawer />
            {media.trailerId && YOUTUBE_ID.test(media.trailerId) && <TrailerPreview media={media} className="hero-btn hero-btn--light" />}
          </>
        }
      >
        {!!media.score && <p className="entry-score">评分 <strong>{(media.score / 10).toFixed(1)}</strong></p>}
        {/* 离线 / 过期时说清作品资料是什么时候的（与发现页同一条细条） */}
        <FreshnessNote freshness={media.freshness} className="work-freshness" />
        {nextAiring && (
          <p className="entry-airing">
            第 {nextAiring.episode} 集 {airingDistance(nextAiring.at)}播出 · <time dateTime={new Date(nextAiring.at * 1000).toISOString()}>{new Date(nextAiring.at * 1000).toLocaleDateString('zh-CN', { weekday: 'long' })}</time>
          </p>
        )}
      </WorkHero>
      <div className="work-body">
        <LoginHint />
        <section className="work-section" aria-labelledby="entry-episodes-heading">
          <div className="work-section-head">
            <h2 id="entry-episodes-heading" className="work-section-title">剧集</h2>
            <Tabs value={panel} onChange={(value) => setPanel(value as Panel)} label="播放来源">
              <Tab value="local">{PANELS.local}</Tab>
              <Tab value="online">{PANELS.online}</Tab>
            </Tabs>
          </div>
          {episodeMetadata.error && <p className="entry-notice" role="status">逐集资料暂不可用，先用作品自带的标题。<button type="button" className="link" onClick={episodeMetadata.retry}>重新加载</button></p>}
          {/* 首次打开后保留来源面板，切换时不丢失选择和请求 */}
          <div role="tabpanel" aria-label={PANELS.local} hidden={panel !== 'local'}>
            <MediaPlayButton media={playbackMedia} inline onPrimaryChange={setPrimary} />
          </div>
          <SourcePanel active={panel === 'online'} label={PANELS.online}><MediaSourceButton media={playbackMedia} inline /></SourcePanel>
        </section>
        <EntryInfo media={media} />
        <EntryRelations media={media} />
      </div>
    </main>
  )
}

/**
 * 作品详情读不出来（离线、目录服务不可用）时，媒体库里这部作品的本地文件仍然能看：
 * 媒体库海报点进来就是这一页，不能让断网把本地播放也一起挡掉。
 */
function LocalFallback({ id }: { id: number }) {
  const [clusters, setClusters] = useState<LibraryCluster[]>([])
  useEffect(() => {
    let alive = true
    fetchLibrary()
      .then((data) => { if (alive) setClusters(data.clusters.filter((c) => clusterWork(c)?.anilistId === id)) })
      .catch((err: unknown) => console.error('读取媒体库失败', err))
    return () => { alive = false }
  }, [id])
  if (clusters.length === 0) return null
  return (
    <p className="work-strip">
      作品资料读不到，媒体库里有这部作品的本地文件，可以直接打开播放：
      {clusters.map((c, i) => <span key={c.clusterKey}>{i > 0 && '、'}<LibraryAnimeLink clusterKey={c.clusterKey} className="link">{c.title}（{c.episodeCount} 集）</LibraryAnimeLink></span>)}
    </p>
  )
}

function SourcePanel({ active, label, children }: { active: boolean; label: string; children: ReactNode }) {
  const [visited, setVisited] = useState(active)
  useEffect(() => { if (active) setVisited(true) }, [active])
  return <div role="tabpanel" aria-label={label} hidden={!active}>{(active || visited) && children}</div>
}
