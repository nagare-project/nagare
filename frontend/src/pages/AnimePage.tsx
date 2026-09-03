import { Link, useParams } from '@tanstack/react-router'
import { useState } from 'react'
import { EpisodeRow } from '../components/library/EpisodeRow'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useLibrary } from '../hooks/useLibrary'
import { usePlayerStatus } from '../hooks/usePlayerStatus'
import { playFile } from '../lib/endpoints'
import { errorText } from '../lib/format'
import { mono } from '../theme'
import type { LibraryCluster } from '../lib/endpoints'
import '../components/library/library.css'

/**
 * `/anime/$clusterKey` 单部作品页：横幅 + 海报 + 剧集列表。
 *
 * 为什么要有这一页：媒体库改成海报网格之后，剧集列表就没有落脚处了。
 * seanime 也是这个结构（网格 → 详情页），原因是列表页一旦超过十来部，
 * 把每一集都摊在首页上会让「找到那部番」这件事本身变难。
 */
export function AnimePage() {
  const { clusterKey } = useParams({ from: '/anime/$clusterKey' })
  const library = useLibrary()
  const player = usePlayerStatus()

  const [pendingFileId, setPendingFileId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function handlePlay(fileId: string): Promise<void> {
    if (pendingFileId !== null) return
    setPendingFileId(fileId)
    setError(null)
    try {
      const result = await playFile(fileId)
      player.apply({
        playing: true,
        fileId,
        title: result.title,
        position: 0,
        duration: 0,
        paused: false,
        danmaku: result.danmaku,
      })
    } catch (err) {
      setError(errorText(err, '播放失败'))
    } finally {
      setPendingFileId(null)
    }
  }

  if (library.state.phase === 'unauthorized') return <UnauthorizedNotice />
  if (library.state.phase === 'loading') {
    return (
      <main className="lib-shell">
        <p className="result result--dim" style={mono} role="status">
          正在读取媒体库 …
        </p>
      </main>
    )
  }
  if (library.state.phase === 'error') {
    return (
      <main className="lib-shell">
        <div className="page-notice">
          <h2 className="page-notice-title">读取媒体库失败</h2>
          <p className="page-notice-copy">{library.state.message}</p>
        </div>
      </main>
    )
  }

  const cluster = library.state.data.clusters.find((c) => c.clusterKey === clusterKey)
  if (cluster === undefined) {
    // 扫描后作品簇的 key 会变（文件增删导致重新归簇），旧链接因此可能失效。
    // 这不是错误，给一条回得去的路就行。
    return (
      <main className="lib-shell">
        <div className="page-notice">
          <h2 className="page-notice-title">找不到这部作品</h2>
          <p className="page-notice-copy">
            它可能已经被移出媒体库，或者重新扫描后归到了别的分组里。
          </p>
          <p className="page-notice-actions">
            <Link to="/" className="btn btn--sm">
              回到媒体库
            </Link>
          </p>
        </div>
      </main>
    )
  }

  const playing = player.status?.playing === true ? player.status : null

  return (
    <main className="lib-shell anime-shell">
      <AnimeHero cluster={cluster} />

      {error !== null && (
        <p className="result result--err" style={mono} role="status" aria-live="polite">
          {error}
        </p>
      )}

      {cluster.groups.map((group) => (
        <section key={group.groupKey} className="anime-group">
          {shouldShowGroupLabel(group.label, cluster.title, cluster.groups.length) && (
            <h2 className="anime-group-label">{group.label}</h2>
          )}
          <ul className="ep-list">
            {group.items.map((item) => (
              <EpisodeRow
                key={item.fileId}
                item={item}
                onPlay={(fileId) => void handlePlay(fileId)}
                isActive={item.fileId === playing?.fileId}
                isPending={item.fileId === pendingFileId}
              />
            ))}
          </ul>
        </section>
      ))}
    </main>
  )
}

/** 顶部横幅：封面模糊铺底 + 清晰海报 + 标题与元信息 */
function AnimeHero({ cluster }: { cluster: LibraryCluster }) {
  const [failed, setFailed] = useState(false)
  const { title, season, episodeCount, cover } = cluster
  const hasCover = cover !== undefined && !failed

  return (
    <header className="anime-hero">
      {hasCover && (
        <div className="anime-hero-bg" aria-hidden="true">
          <img src={cover} alt="" onError={() => setFailed(true)} />
        </div>
      )}
      <div className="anime-hero-body">
        <span className="anime-hero-art">
          {hasCover ? (
            <img src={cover} alt="" onError={() => setFailed(true)} />
          ) : (
            <span className="poster-art-blank" aria-hidden="true">
              流
            </span>
          )}
        </span>
        <div className="anime-hero-text">
          <Link to="/" className="link anime-back">
            ← 媒体库
          </Link>
          <h1 className="anime-title">{title}</h1>
          <p className="anime-meta">
            {season !== null ? `第 ${season} 季 · ` : ''}
            {episodeCount} 集
          </p>
        </div>
      </div>
    </header>
  )
}

/** 只有一个分组且标签与作品同名（或为空）时不重复展示分组标题 */
function shouldShowGroupLabel(groupLabel: string, clusterTitle: string, groupCount: number): boolean {
  if (groupLabel === '') return false
  if (groupCount > 1) return true
  return groupLabel !== clusterTitle
}
