import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { ContinueSection } from '../components/library/ContinueSection'
import { PosterGrid } from '../components/library/PosterGrid'
import { GettingStarted } from '../components/library/GettingStarted'
import { NowPlayingBar } from '../components/library/NowPlayingBar'
import { ScanDrops } from '../components/library/ScanDrops'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useLibrary } from '../hooks/useLibrary'
import { usePlayerStatus } from '../hooks/usePlayerStatus'
import { useSettings } from '../hooks/useSettings'
import { pausePlayer, playFile, stopPlayer } from '../lib/endpoints'
import type { LibraryState } from '../hooks/useLibrary'
import type { SettingsState } from '../hooks/useSettings'
import { errorText, formatClock } from '../lib/format'
import { mono } from '../theme'
import '../components/library/library.css'

/** 顶栏状态行的一条消息；link 是附带的恢复动作（目前只有「去设置安装 mpv」） */
interface Notice {
  tone: 'dim' | 'ok' | 'err'
  text: string
  link?: { to: '/settings'; label: string }
}

/** mpv 缺失时的恢复动作：设置页有安装指引 + 一键复制 + 重新检测 */
const INSTALL_MPV_LINK = { to: '/settings', label: '去设置安装 mpv →' } as const

/**
 * `/` 媒体库主页：顶栏（品牌 + mpv 状态 + 重扫 + 设置）、簇列表 /
 * 空态引导、底部正在播放条。token 获取仍走 main.tsx 的 acquireToken，
 * 这里只负责把 401 转成「走启动链接重新进入」的提示。
 */
export function LibraryPage() {
  const library = useLibrary()
  const settings = useSettings()
  const player = usePlayerStatus()

  const [notice, setNotice] = useState<Notice | null>(null)
  const [pendingFileId, setPendingFileId] = useState<string | null>(null)
  const [rescanBusy, setRescanBusy] = useState(false)
  const [playerBusy, setPlayerBusy] = useState(false)

  async function handlePlay(fileId: string): Promise<void> {
    if (pendingFileId !== null) return
    setPendingFileId(fileId)
    setNotice(null)
    try {
      const result = await playFile(fileId)
      // 乐观更新：先用 play 响应立起播放条（含弹幕状态），时长等 2s 后首拍轮询校正
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
      console.error('播放失败', err)
      // mpv 没装时播放必失败，错误旁直接给出去处，别让用户去猜
      const isMpvMissing = settings.state.phase === 'ready' && !settings.state.data.mpv.found
      setNotice({
        tone: 'err',
        text: errorText(err, '播放失败'),
        ...(isMpvMissing ? { link: INSTALL_MPV_LINK } : {}),
      })
    } finally {
      setPendingFileId(null)
    }
  }

  async function handleTogglePause(paused: boolean): Promise<void> {
    setPlayerBusy(true)
    try {
      await pausePlayer(paused)
      await player.refresh()
    } catch (err) {
      console.error('暂停/继续失败', err)
      setNotice({ tone: 'err', text: errorText(err, '暂停/继续失败') })
    } finally {
      setPlayerBusy(false)
    }
  }

  async function handleStop(): Promise<void> {
    setPlayerBusy(true)
    try {
      await stopPlayer()
      await player.refresh()
    } catch (err) {
      console.error('停止播放失败', err)
      setNotice({ tone: 'err', text: errorText(err, '停止播放失败') })
    } finally {
      setPlayerBusy(false)
    }
  }

  async function handleRescan(): Promise<void> {
    setRescanBusy(true)
    setNotice({ tone: 'dim', text: '正在重新扫描 …' })
    try {
      const stats = await library.rescan()
      setNotice({ tone: 'ok', text: `扫描完成 · ${stats.videos} 个视频 / ${stats.clusters} 部作品` })
    } catch (err) {
      console.error('重新扫描失败', err)
      setNotice({ tone: 'err', text: errorText(err, '重新扫描失败') })
    } finally {
      setRescanBusy(false)
    }
  }

  if (library.state.phase === 'unauthorized' || settings.state.phase === 'unauthorized') {
    return <UnauthorizedNotice />
  }

  const playing = player.status?.playing === true ? player.status : null
  const canRescan =
    library.state.phase === 'ready' && library.state.data.folders.length > 0 && !rescanBusy
  const statusLine = pickStatusLine(notice, player.error, library.state)

  return (
    <main
      className={playing !== null ? 'lib-shell lib-shell--with-bar' : 'lib-shell'}
    >
      <header className="page-head">
        <h1 className="page-title">媒体库</h1>
        <MpvChip state={settings.state} />
        <span className="page-head-spacer" />
        <div className="page-head-actions">
          <button
            type="button"
            className="btn btn--sm"
            onClick={() => void handleRescan()}
            disabled={!canRescan}
          >
            {rescanBusy ? '扫描中 …' : '重新扫描'}
          </button>
        </div>
      </header>

      <MpvAlert state={settings.state} />

      <p
        className={statusLine === null ? 'result lib-status' : `result lib-status result--${statusLine.tone}`}
        style={mono}
        role="status"
        aria-live="polite"
      >
        {statusLine?.text}
        {statusLine?.link !== undefined && (
          <Link to={statusLine.link.to} className="link lib-status-link">
            {statusLine.link.label}
          </Link>
        )}
      </p>

      {/* 有内容时挂在这里；一部作品都没有时改由空态摊开（见 LibraryBody），
          否则同一条信息会在一屏里出现两次 */}
      {library.state.phase === 'ready' && library.state.data.clusters.length > 0 && (
        <ScanDrops folders={library.state.data.folders} />
      )}

      <LibraryBody
        state={library.state}
        settingsState={settings.state}
        onRetry={() => void library.reload()}
        onAdd={library.addFolder}
        onPlay={(fileId) => void handlePlay(fileId)}
        activeFileId={playing?.fileId ?? null}
        pendingFileId={pendingFileId}
      />

      {playing !== null && (
        <NowPlayingBar
          status={playing}
          busy={playerBusy}
          onTogglePause={(paused) => void handleTogglePause(paused)}
          onStop={() => void handleStop()}
        />
      )}
    </main>
  )
}

/** mpv 状态点：ready 且找到 → 亮青；找不到 → 红；其余（加载中/失败）→ 灰 */
function MpvChip({ state }: { state: SettingsState }) {
  const mpv = state.phase === 'ready' ? state.data.mpv : null
  const dotClass =
    mpv === null ? 'mpv-dot' : mpv.found ? 'mpv-dot mpv-dot--ok' : 'mpv-dot mpv-dot--missing'
  const title =
    mpv === null
      ? 'mpv 状态未知'
      : mpv.found
        ? `mpv 已就绪${mpv.version !== undefined ? ` · ${mpv.version}` : ''}`
        : 'mpv 未找到'
  return (
    <span className="mpv-chip" title={title}>
      <span className={dotClass} aria-hidden="true" />
      mpv
    </span>
  )
}

/** mpv 缺失时的显眼提示条：一句 hint + 去设置页（安装指引 / 复制命令 / 重新检测都在那） */
function MpvAlert({ state }: { state: SettingsState }) {
  if (state.phase !== 'ready' || state.data.mpv.found) return null
  return (
    <p className="alert-warn" role="alert">
      未检测到 mpv，无法播放。{state.data.mpv.hint ?? '请先安装 mpv。'}{' '}
      <Link to={INSTALL_MPV_LINK.to} className="link alert-warn-link">
        {INSTALL_MPV_LINK.label}
      </Link>
    </p>
  )
}

interface LibraryBodyProps {
  state: LibraryState
  /** 引导屏要据此显示 mpv 与账号的就绪状态 */
  settingsState: SettingsState
  onRetry: () => void
  onAdd: ReturnType<typeof useLibrary>['addFolder']
  onPlay: (fileId: string) => void
  activeFileId: string | null
  pendingFileId: string | null
}

/** 主体三态：loading / error / ready（ready 内再分空态、无视频、簇列表） */
function LibraryBody({
  state,
  settingsState,
  onRetry,
  onAdd,
  onPlay,
  activeFileId,
  pendingFileId,
}: LibraryBodyProps) {
  if (state.phase === 'loading') {
    return (
      <p className="result result--dim" style={mono}>
        正在读取媒体库 …
      </p>
    )
  }

  if (state.phase === 'error') {
    return (
      <div className="page-notice">
        <h2 className="page-notice-title">读取媒体库失败</h2>
        <p className="page-notice-copy result--err">{state.message}</p>
        <p className="page-notice-actions">
          <button type="button" className="btn btn--sm" onClick={onRetry}>
            重试
          </button>
        </p>
      </div>
    )
  }

  // 此处 state.phase 只剩 'ready'（unauthorized 已在页面层拦截）
  if (state.phase !== 'ready') return null
  const { folders, clusters, continueWatching } = state.data

  if (folders.length === 0) {
    // 首次运行：不把「添加文件夹」摆成唯一入口（磁力那条路不需要它）
    return <GettingStarted settings={settingsState} onAdd={onAdd} />
  }

  if (clusters.length === 0) {
    // 一个都没扫到时，「为什么」就是这一页的全部内容 —— 所以默认展开。
    // 顶上那份此时不渲染（见页面里的 clusters.length > 0 判断）。
    const dropped = folders.reduce((n, f) => n + (f.dropped?.total ?? 0), 0)
    return (
      <div className="page-notice">
        <h2 className="page-notice-title">没有发现视频文件</h2>
        <p className="page-notice-copy">
          {dropped > 0
            ? `已添加 ${folders.length} 个文件夹，扫描到的 ${dropped} 项都被跳过了。`
            : `已添加 ${folders.length} 个文件夹，但里面没有扫描到视频。检查路径是否正确，或到「设置」里调整文件夹后重新扫描。`}
        </p>
        {dropped > 0 && (
          <div className="page-notice-detail">
            <ScanDrops folders={folders} open />
          </div>
        )}
      </div>
    )
  }

  return (
    <>
      <ContinueSection
        items={continueWatching}
        onPlay={onPlay}
        activeFileId={activeFileId}
        pendingFileId={pendingFileId}
      />
      <PosterGrid clusters={clusters} />
    </>
  )
}

/** 状态行优先级：主动消息（扫描/播放反馈）> 播放器轮询错误 > 上次扫描时间 */
function pickStatusLine(
  notice: Notice | null,
  playerError: string | null,
  libState: LibraryState,
): Notice | null {
  if (notice !== null) return notice
  if (playerError !== null) return { tone: 'err', text: playerError }
  if (libState.phase === 'ready' && libState.data.scannedAt !== null) {
    return { tone: 'dim', text: `上次扫描 ${formatClock(libState.data.scannedAt)}` }
  }
  return null
}
