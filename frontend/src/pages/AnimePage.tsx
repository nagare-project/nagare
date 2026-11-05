import { Link, useParams } from '@tanstack/react-router'
import { useRef, useState } from 'react'
import { Icon } from '../components/ui/Icon'
import { AssociationPanel } from '../components/library/AssociationPanel'
import { EpisodeList } from '../components/library/EpisodeList'
import { WorkHero } from '../components/library/WorkHero'
import { LoginHint, WorkHeroSkeleton } from '../components/library/WorkStates'
import { MediaEntryLink } from '../components/media/MediaEntryLink'
import { nextLibraryEpisode, primaryLabel } from '../components/media/MediaPlayButton'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useLibrary } from '../hooks/useLibrary'
import { usePlayerStatus } from '../hooks/usePlayerStatus'
import { clusterDisplayTitle } from '../lib/associations'
import { inFlight } from '../lib/busy'
import { playFile } from '../lib/endpoints'
import { errorText } from '../lib/format'
import { clusterWork } from '../lib/librarySeries'
import { posterToneStyle } from '../lib/posterTones'
import { mono } from '../theme'
import '../components/library/library.css'

/**
 * `/anime/$clusterKey` 媒体库里一个本地分组的页面（DESIGN.md「作品页」）：与目录作品页共用一套结构 ——
 * hero（海报 + 标题 + 动作行）→ 对应作品 → 集数列表。没认出作品的没有海报，标题取自文件名，
 * 主按钮是「认作品」（打开对应作品面板的选作品对话框）。
 *
 * clusterKey 会随重新扫描变化（文件增删导致重新归簇），旧链接因此可能失效：那是正常的，给一条回得去的路。
 */
export function AnimePage() {
  const { clusterKey } = useParams({ from: '/anime/$clusterKey' })
  const library = useLibrary()
  const player = usePlayerStatus()
  const pickOpener = useRef<HTMLButtonElement>(null)
  const [pickRequest, setPickRequest] = useState(0)
  const [pendingFileId, setPendingFileId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function handlePlay(fileId: string): Promise<void> {
    if (pendingFileId !== null) return
    setPendingFileId(fileId)
    setError(null)
    try {
      const result = await playFile(fileId)
      player.apply({ playing: true, fileId, title: result.title, position: 0, duration: 0, paused: false, danmaku: result.danmaku })
    } catch (err) {
      setError(errorText(err, '播放失败'))
    } finally {
      setPendingFileId(null)
    }
  }

  if (library.state.phase === 'unauthorized') return <UnauthorizedNotice />
  if (library.state.phase === 'loading') {
    return (
      <main className="work-page">
        <WorkHeroSkeleton label="正在读取媒体库" />
      </main>
    )
  }
  if (library.state.phase === 'error') {
    return (
      <main className="lib-shell">
        <div className="page-notice">
          <h1 className="page-notice-title">读取媒体库失败</h1>
          <p className="page-notice-copy">{library.state.message}</p>
          <p className="page-notice-actions"><button type="button" className="btn btn--sm" onClick={() => void library.reload()}>重试</button></p>
        </div>
      </main>
    )
  }

  const cluster = library.state.data.clusters.find((c) => c.clusterKey === clusterKey)
  if (cluster === undefined) {
    return (
      <main className="lib-shell">
        <div className="page-notice">
          <h1 className="page-notice-title">找不到这部作品</h1>
          <p className="page-notice-copy">它可能已经被移出媒体库，或者重新扫描后归到了别的分组里。</p>
          <p className="page-notice-actions"><Link to="/library" className="btn btn--sm">回到媒体库</Link></p>
        </div>
      </main>
    )
  }

  const playing = player.status?.playing === true ? player.status : null
  const next = nextLibraryEpisode(cluster)
  const title = clusterDisplayTitle(cluster)
  const work = clusterWork(cluster)
  // 没认出：没认定过、也没自动认出（标为「不是目录里的作品」的算认过）
  const unrecognized = cluster.association === undefined && cluster.matched === undefined
  const tones = posterToneStyle(cluster.posterTones)
  const pending = pendingFileId !== null
  const playButton = next && (
    <button type="button" className={unrecognized ? 'hero-btn hero-btn--heavy' : 'hero-btn hero-btn--solid'} {...inFlight(pending, () => void handlePlay(next.fileId))}>
      <Icon name="play" size={16} />{pending ? '正在启动…' : primaryLabel(next)}
    </button>
  )

  return (
    <main className="work-page" style={tones}>
      <WorkHero
        cover={cluster.cover}
        showPoster={!unrecognized || cluster.cover !== undefined}
        backdrop={cluster.cover}
        backdropIsCover
        toned={tones !== undefined}
        title={title}
        meta={[cluster.season !== null ? `第 ${cluster.season} 季` : null, `${cluster.episodeCount} 集`, title !== cluster.title ? `文件夹：${cluster.title}` : null]}
        back={<Link to="/library" className="work-hero-back"><Icon name="left" size={16} />媒体库</Link>}
        actions={
          <>
            {unrecognized && (
              <button ref={pickOpener} type="button" className="hero-btn hero-btn--solid" aria-haspopup="dialog" onClick={() => setPickRequest((n) => n + 1)}>
                <Icon name="search" size={16} />认作品
              </button>
            )}
            {playButton}
            {work && <MediaEntryLink id={work.anilistId} className="hero-btn hero-btn--light"><Icon name="info" size={16} />作品详情</MediaEntryLink>}
          </>
        }
      />
      <div className="work-body">
        {error !== null && <p className="result result--err" style={mono} role="status" aria-live="polite">{error}</p>}
        <LoginHint />
        {/* 同一个路由组件在作品之间切换时会复用：按分组重置面板状态 */}
        <AssociationPanel key={cluster.clusterKey} cluster={cluster} onChanged={library.reload} pickRequest={pickRequest} pickOpener={pickOpener} />
        <section className="work-section" aria-labelledby="anime-episodes-heading">
          <h2 id="anime-episodes-heading" className="work-section-title">本地剧集<small>{cluster.episodeCount} 集已入库</small></h2>
          <EpisodeList cluster={cluster} next={next} pending={pending} {...(playing ? { activeFileId: playing.fileId } : {})} onPlay={(item) => void handlePlay(item.fileId)} />
        </section>
      </div>
    </main>
  )
}
