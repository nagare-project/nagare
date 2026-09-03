import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from '@tanstack/react-router'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useLibrary } from '../hooks/useLibrary'
import { mono } from '../theme'
import type { LibraryItem } from '../lib/endpoints'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/watch/$fileId` 浏览器内播放。
 *
 * ⚠️ 决议 A5 原本明确不做这件事（不解码、不做浏览器内播放，一律交给 mpv）。
 * 用户 2026-09-03 要求接上，所以有了这一页 —— 但它的定位是【补充路径】，
 * 不是替代：后端只把字节原样喂给 <video>，没有转码。
 *
 * 也就是说能不能播完全取决于浏览器：
 *   - MP4 里的 H.264 + AAC —— 基本都能播
 *   - MKV 容器 —— Chrome/Safari 多半不认
 *   - HEVC / AV1 —— 看浏览器与硬件，Safari 对 MP4 里的 HEVC 支持较好
 *
 * 所以失败是【常态而非异常】，这一页最重要的一件事是：播不了的时候
 * 说清楚为什么、并把用户送回 mpv，而不是留一块黑屏。
 */
export function WatchPage() {
  const { fileId } = useParams({ from: '/watch/$fileId' })
  const library = useLibrary()
  const videoRef = useRef<HTMLVideoElement>(null)
  const [failed, setFailed] = useState(false)

  // 换一集时把失败状态清掉，否则上一集播不了会把下一集也标成失败
  useEffect(() => {
    setFailed(false)
  }, [fileId])

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

  const found = findItem(library.state.data.clusters, fileId)
  if (found === null) {
    return (
      <main className="lib-shell">
        <div className="page-notice">
          <h2 className="page-notice-title">找不到这个文件</h2>
          <p className="page-notice-copy">
            它可能已经被移走或改名（文件 id 挂在 名称+大小+修改时间 上）。重新扫描一次即可。
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

  const { item, clusterKey, title } = found

  return (
    <main className="lib-shell watch-shell">
      <header className="page-head">
        <Link to="/anime/$clusterKey" params={{ clusterKey }} className="link">
          ← {title}
        </Link>
      </header>

      {item.stream === undefined ? (
        <Unplayable
          reason="这个构建没有挂载媒体端点，浏览器内播放不可用。"
          clusterKey={clusterKey}
        />
      ) : failed ? (
        <Unplayable
          reason="浏览器放不了这个文件的编码。nagare 不转码，只是把原始字节喂给播放器 —— MKV 容器、HEVC 与 AV1 在多数浏览器上都会到这一步。"
          clusterKey={clusterKey}
        />
      ) : (
        <video
          ref={videoRef}
          className="watch-video"
          src={item.stream}
          controls
          autoPlay
          onError={() => setFailed(true)}
        >
          {/* 字幕轨不走这里：外挂 ASS 与弹幕轨都是 mpv 那条路的能力 */}
        </video>
      )}

      <p className="watch-file" style={mono}>
        {item.fileName}
      </p>

      <p className="result result--dim">
        浏览器内播放是<strong>补充路径</strong>，没有弹幕、没有 ASS 字幕、进度也不回写账号。
        完整体验请用 mpv 从媒体库播放。
      </p>
    </main>
  )
}

function Unplayable({ reason, clusterKey }: { reason: string; clusterKey: string }) {
  return (
    <div className="page-notice">
      <h2 className="page-notice-title">这里播不了</h2>
      <p className="page-notice-copy">{reason}</p>
      <p className="page-notice-actions">
        <Link to="/anime/$clusterKey" params={{ clusterKey }} className="btn btn--sm btn--primary">
          回去用 mpv 播
        </Link>
      </p>
    </div>
  )
}

/** 在所有作品簇里按 fileId 找条目，顺带带出它所属的簇（用于返回链接） */
function findItem(
  clusters: { clusterKey: string; title: string; groups: { items: LibraryItem[] }[] }[],
  fileId: string,
): { item: LibraryItem; clusterKey: string; title: string } | null {
  for (const c of clusters) {
    for (const g of c.groups) {
      for (const item of g.items) {
        if (item.fileId === fileId) {
          return { item, clusterKey: c.clusterKey, title: c.title }
        }
      }
    }
  }
  return null
}
