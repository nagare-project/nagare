import { useEffect, useState } from 'react'
import { useSearch } from '@tanstack/react-router'
import { MediaDetails, MediaExternalLinks, MediaRelations } from '../components/media/MediaDetails'
import { MediaArtwork } from '../components/media/MediaArtwork'
import { MediaPlayButton } from '../components/media/MediaPlayButton'
import { MediaSourceButton } from '../components/media/MediaSourceButton'
import { MediaTorrentButton } from '../components/media/MediaTorrentButton'
import { TrailerPreview } from '../components/media/MediaPreview'
import { useMediaDetails } from '../components/media/useMediaDetails'
import { Tabs, Tab } from '../components/ui'
import '../components/media/media-preview.css'
import './entry.css'

const FORMATS: Record<string, string> = { TV: 'TV', TV_SHORT: '短篇动画', MOVIE: '剧场版', SPECIAL: '特别篇', OVA: 'OVA', ONA: '网络动画' }
const SOURCES: Record<string, string> = { ORIGINAL: '原创', MANGA: '漫画', LIGHT_NOVEL: '轻小说', NOVEL: '小说', VIDEO_GAME: '游戏', VISUAL_NOVEL: '视觉小说', WEB_NOVEL: '网络小说', OTHER: '其他' }

/** 目录详情使用真实 AniList 元数据；播放入口复用实际媒体库选集。 */
export function EntryPage() {
  const { id } = useSearch({ from: '/entry' })
  const { media, loading, error, retry } = useMediaDetails(id)
  const [bannerOpacity, setBannerOpacity] = useState(1)
  useEffect(() => { const scroll = () => setBannerOpacity(Math.max(0, 1 - window.scrollY / 480)); scroll(); window.addEventListener('scroll', scroll, { passive: true }); return () => window.removeEventListener('scroll', scroll) }, [])
  const [tab, setTab] = useState('local')
  useEffect(() => { window.scrollTo(0, 0); setTab('local') }, [id])
  useEffect(() => {
    const previous = document.title
    if (media) document.title = `${media.title} · nagare`
    return () => { document.title = previous }
  }, [media])
  return <main className="catalog-entry lib-shell">
    {loading && <div className="media-detail-loading" role="status" aria-label="正在加载作品详情" />}
    {error && <p className="result result--err" role="alert">{error} <button className="btn" onClick={retry}>重试</button> <a className="link" href="/discover">返回发现</a></p>}
    {media && <>
      <div className="catalog-entry-backdrop" style={{ opacity: bannerOpacity }} aria-hidden="true"><MediaArtwork src={media.banner ?? media.cover} title={media.title} /></div>
      <MediaDetails media={media} page><div className="media-preview-actions"><MediaExternalLinks media={media} />{media.trailerId && <TrailerPreview media={media} />}</div></MediaDetails>
      <Tabs value={tab} onChange={setTab} label="作品内容"><Tab value="local">本地媒体库</Tab><Tab value="details">作品信息</Tab></Tabs>
      {tab === 'local' ? <section className="catalog-entry-local"><h2>播放</h2><p>可以从媒体库继续观看，从已安装的本地插件在线找源，或用本机规则搜索磁力资源。</p><div className="discover-card-play-row"><MediaPlayButton media={media} onOpenChange={() => {}} /><MediaSourceButton media={media} /><MediaTorrentButton media={media} /></div><a className="link" href="/">查看媒体库</a></section> : <dl className="catalog-entry-facts">
        {Object.entries({ '类型': FORMATS[media.format ?? ''] || media.format, '原作': SOURCES[media.source ?? ''] || media.source, '集数': media.episodes, '单集时长': media.duration ? `${media.duration} 分钟` : undefined, '首播日期': media.startDate, '制作公司': media.studios?.join(' / '), '原名': media.titleNative }).map(([label, value]) => value ? <div key={label}><dt>{label}</dt><dd>{value}</dd></div> : null)}
      </dl>}
      <MediaRelations media={media} />
    </>}
  </main>
}
