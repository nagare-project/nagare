import { type ReactNode, useEffect, useRef, useState } from 'react'
import { useSearch } from '@tanstack/react-router'
import { MediaDetails, MediaExternalLinks, MediaRelations } from '../components/media/MediaDetails'
import { MediaArtwork } from '../components/media/MediaArtwork'
import { MediaPlayButton } from '../components/media/MediaPlayButton'
import { MediaSourceButton } from '../components/media/MediaSourceButton'
import { MediaTorrentButton } from '../components/media/MediaTorrentButton'
import { TrailerPreview } from '../components/media/MediaPreview'
import { useMediaDetails } from '../components/media/useMediaDetails'
import { useEpisodeMetadata } from '../components/media/useEpisodeMetadata'
import { useCollection } from '../components/media/CollectionContext'
import type { MediaSummary } from '../components/media/types'
import { airingDistance } from '../components/media/media-format'
import { nextEpisodeAiring } from '../components/media/episode-metadata'
import { Icon } from '../components/ui/Icon'
import { Tabs, Tab } from '../components/ui'
import '../components/media/media-preview.css'
import './entry.css'

const FORMATS: Record<string, string> = { TV: 'TV', TV_SHORT: '短篇动画', MOVIE: '剧场版', SPECIAL: '特别篇', OVA: 'OVA', ONA: '网络动画' }
const SOURCES: Record<string, string> = { ORIGINAL: '原创', MANGA: '漫画', LIGHT_NOVEL: '轻小说', NOVEL: '小说', VIDEO_GAME: '游戏', VISUAL_NOVEL: '视觉小说', WEB_NOVEL: '网络小说', OTHER: '其他' }
const TAB_LABELS: Record<string, string> = { local: '本地媒体库', torrent: '磁力播放', online: '在线播放' }

export function EntryPage() {
  const { id } = useSearch({ from: '/entry' })
  const { media, loading, error, retry } = useMediaDetails(id)
  return <main className="catalog-entry lib-shell">
    {loading && <div className="media-detail-loading" role="status" aria-label="正在加载作品详情" />}
    {error && <p className="result result--err" role="alert">{error} <button className="btn" onClick={retry}>重试</button> <a className="link" href="/discover">返回发现</a></p>}
    {media && <EntryContent key={id} media={media} />}
  </main>
}

function EntryContent({ media }: { media: MediaSummary }) {
  const [tab, setTab] = useState('local')
  const actions = useRef<HTMLDetailsElement>(null)
  const information = useRef<HTMLDialogElement>(null)
  const [bannerOpacity, setBannerOpacity] = useState(1)
  const collection = useCollection()
  const watched = collection.entries.find(entry => entry.anilistId === media.id)?.currentEpisode ?? media.watched
  const episodeMetadata = useEpisodeMetadata(media.id)
  const titles = new Map(media.episodeTitles?.map(ep => [ep.episode, ep]))
  for (const ep of episodeMetadata.episodes) titles.set(ep.episode, { ...ep, title: titles.get(ep.episode)?.title || ep.title })
  const nextAiring = nextEpisodeAiring(media, episodeMetadata.episodes)
  const playbackMedia = { ...media, watched, episodeTitles: [...titles.values()] }

  useEffect(() => {
    window.scrollTo(0, 0)
    const previous = document.title
    document.title = `${media.title} · nagare`
    const scroll = () => setBannerOpacity(Math.max(0, 1 - window.scrollY / 480))
    scroll()
    window.addEventListener('scroll', scroll, { passive: true })
    return () => { document.title = previous; window.removeEventListener('scroll', scroll) }
  }, [media.id, media.title])

  return <>
    <div className="catalog-entry-backdrop" style={{ opacity: bannerOpacity }} aria-hidden="true">
      <MediaArtwork src={media.banner ?? media.cover} title={media.title} />
    </div>
    <header className="catalog-entry-header">
      <MediaDetails media={media} page>
        <div className="media-preview-actions"><MediaExternalLinks media={media} />{media.trailerId && <TrailerPreview media={media} />}</div>
      </MediaDetails>
    </header>
    <div className="catalog-entry-toolbar">
      <details ref={actions} className="catalog-entry-tools"><summary aria-label="作品操作"><span aria-hidden="true">⋮</span></summary><div className="catalog-entry-menu">
        <a className="icon-button" href="/" aria-label="管理本地媒体库" title="管理本地媒体库"><Icon name="folder" size={18} />管理本地媒体库</a>
        <button type="button" className="icon-button" onClick={() => { if (actions.current) actions.current.open = false; information.current?.showModal() }} aria-label="作品信息" title="作品信息"><Icon name="info" size={18} />作品信息</button>
      </div></details>
      <Tabs value={tab} onChange={setTab} label="播放来源">
        <Tab value="local"><Icon name="lists" size={18} />本地媒体库</Tab>
        <Tab value="torrent"><Icon name="monitor" size={18} />磁力播放</Tab>
        <Tab value="online"><Icon name="broadcast" size={18} />在线播放</Tab>
      </Tabs>
    </div>
    {nextAiring && <p className="catalog-entry-airing"><strong>第 {nextAiring.episode} 集 {airingDistance(nextAiring.at)}播出</strong><span><Icon name="calendar" size={16} /><time dateTime={new Date(nextAiring.at * 1000).toISOString()}>{new Date(nextAiring.at * 1000).toLocaleDateString('zh-CN', { weekday: 'long' })}</time></span></p>}
    <section className="catalog-entry-content" aria-label={TAB_LABELS[tab]}>
      {episodeMetadata.error && <p className="episode-art-notice" role="status">逐集图片暂不可用，正在使用作品封面。<button type="button" className="link" onClick={episodeMetadata.retry}>重新加载图片</button></p>}
      {/* 首次打开后保留来源面板，切换时不丢失选择和请求。 */}
      <div hidden={tab !== 'local'}><MediaPlayButton media={playbackMedia} inline /></div>
      <SourcePanel active={tab === 'torrent'}><MediaTorrentButton media={playbackMedia} inline /></SourcePanel>
      <SourcePanel active={tab === 'online'}><MediaSourceButton media={playbackMedia} inline /></SourcePanel>
      <dialog ref={information} className="media-description-dialog catalog-entry-information" aria-label="作品信息" onClick={event => { if (event.target === information.current) information.current.close() }}><button type="button" className="icon-button" aria-label="关闭作品信息" onClick={() => information.current?.close()}><Icon name="close" /></button><h2>作品信息</h2><dl className="catalog-entry-facts">
        {Object.entries({ '类型': FORMATS[media.format ?? ''] || media.format, '原作': SOURCES[media.source ?? ''] || media.source, '集数': media.episodes, '单集时长': media.duration ? `${media.duration} 分钟` : undefined, '首播日期': media.startDate, '制作公司': media.studios?.join(' / '), '原名': media.titleNative }).map(([label, value]) => value ? <div key={label}><dt>{label}</dt><dd>{value}</dd></div> : null)}
      </dl></dialog>
    </section>
    <MediaRelations media={media} />
  </>
}

function SourcePanel({ active, children }: { active: boolean; children: ReactNode }) {
  const [visited, setVisited] = useState(active)
  useEffect(() => { if (active) setVisited(true) }, [active])
  return <div hidden={!active}>{(active || visited) && children}</div>
}
