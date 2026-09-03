import type { TorrentPlayState } from '../../hooks/useTorrentPlay'
import type { TorrentPhase, TorrentStatus } from '../../lib/endpoints'
import { formatRate, ratioPercent } from '../../lib/format'
import { mono } from '../../theme'
import './torrent.css'

/**
 * 连续多少秒没有分享者才升级成显式警告。
 * 刚开始找 peer 时 0 是正常的，几秒内就报警会变成噪音；但一直是 0
 * 就是「这条资源没人做种」的唯一信号，必须让用户看见并能据此换资源。
 */
export const ZERO_PEER_ALERT_SECONDS = 15

/** 后端 phase → 中文进度文案（buffering 另外拼百分比） */
export const PHASE_TEXT: Record<TorrentPhase, string> = {
  idle: '正在启动 …',
  metadata: '正在查找分享者 …',
  selecting: '正在读取种子信息 …',
  buffering: '正在缓冲',
  ready: '即将开始播放',
}

export interface TorrentStatusBarProps {
  state: TorrentPlayState
  /** 轮询到的后端状态；null = 尚未拉到或上次拉取失败 */
  status: TorrentStatus | null
  /** 已连续多少秒没有分享者 */
  zeroPeerSeconds: number
  /** 取消缓冲 / 停止播放 / 关闭错误 */
  onCancel: () => void
  /** 失败后重试同一条磁力 */
  onRetry: () => void
}

/**
 * 底部固定的磁力状态条：分阶段文案 + 缓冲进度 + 分享者数 + 速度 + 取消。
 *
 * 三种形态：
 * - starting / selecting：完整缓冲态，进度条 + 阶段文案 + 可取消；
 * - streaming：收敛成轻量指示（mpv 已接管播放，但下载还在继续，速度与分享者数
 *   仍是判断「会不会卡住」的唯一依据，所以保留，只去掉起播缓冲进度条）；
 * - error：后端中文错误原样显示 + 重试 / 关闭。
 */
export function TorrentStatusBar({
  state,
  status,
  zeroPeerSeconds,
  onCancel,
  onRetry,
}: TorrentStatusBarProps) {
  if (state.phase === 'idle') return null

  if (state.phase === 'error') {
    return (
      <footer className="tsb-bar tsb-bar--error" aria-label="磁力播放失败">
        <span className="tsb-dot tsb-dot--error" aria-hidden="true" />
        <div className="tsb-main">
          <p className="tsb-phase result--err" role="alert">
            {state.message}
          </p>
          <p className="tsb-title" title={state.title}>
            {state.title}
          </p>
        </div>
        <div className="tsb-actions">
          <button type="button" className="btn btn--sm" onClick={onRetry}>
            重试
          </button>
          <button
            type="button"
            className="btn btn--sm btn--danger"
            onClick={onCancel}
          >
            关闭
          </button>
        </div>
      </footer>
    )
  }

  const streaming = state.phase === 'streaming'
  const bufferedPct = ratioPercent(status?.buffered ?? 0)
  const progressPct = ratioPercent(status?.progress ?? 0)

  return (
    <footer
      className={streaming ? 'tsb-bar tsb-bar--streaming' : 'tsb-bar'}
      aria-label={streaming ? '正在边下边播' : '磁力缓冲进度'}
    >
      {!streaming && (
        <div className="tsb-track" aria-hidden="true">
          <div className="tsb-track-fill" style={{ width: `${bufferedPct}%` }} />
        </div>
      )}

      <span className="tsb-dot" aria-hidden="true" />

      <div className="tsb-main">
        <p className="tsb-phase" style={mono}>
          {streaming ? '正在边下边播' : phaseText(state, status, bufferedPct)}
        </p>
        <p className="tsb-title" title={displayTitle(state, status)}>
          {displayTitle(state, status)}
        </p>
      </div>

      {/* 播报只给粗粒度阶段：缓冲百分比每秒变一次，挂在 live region 上会让读屏一秒念一遍 */}
      <span className="visually-hidden" role="status" aria-live="polite">
        {coarsePhase(state, status)}
      </span>

      {!streaming && (
        <span
          className="visually-hidden"
          role="progressbar"
          aria-label="起播缓冲进度"
          aria-valuenow={bufferedPct}
          aria-valuemin={0}
          aria-valuemax={100}
        />
      )}

      <PeerReadout status={status} streaming={streaming} progressPct={progressPct} />

      <div className="tsb-actions">
        <button
          type="button"
          className="btn btn--sm btn--danger"
          onClick={onCancel}
        >
          {streaming ? '停止' : '取消'}
        </button>
      </div>

      {/* 会话级失败（目前只有写盘失败）压过分享者告警：磁盘写不进去时，
          「没有分享者」这条提示会把用户引向完全错误的下一步。 */}
      {status?.error !== undefined && status.error !== '' && (
        <p className="tsb-alert result--err" role="alert">
          {status.error}
        </p>
      )}

      {(status?.error === undefined || status.error === '') &&
        zeroPeerSeconds >= ZERO_PEER_ALERT_SECONDS && (
          <p className="tsb-alert result--warn" role="alert">
            一直没有连接到任何分享者
            <span aria-hidden="true">（已 {zeroPeerSeconds} 秒）</span>
            {streaming
            ? '，播放可能会卡住。等一会儿仍是 0 就停止，换一条资源。'
            : '。这条资源可能已经没人做种了，建议取消后换一条。'}
          </p>
        )}
    </footer>
  )
}

/** idle / error 在组件里已经提前返回，这两个 helper 只会看到剩下三个阶段 */
type ActivePlayState = Extract<
  TorrentPlayState,
  { phase: 'starting' | 'selecting' | 'streaming' }
>

/** 缓冲期的阶段文案：selecting 阶段以前端状态为准（后端已在等用户操作） */
function phaseText(state: ActivePlayState, status: TorrentStatus | null, bufferedPct: number): string {
  if (state.phase === 'selecting') return '请选择要播放的剧集'
  if (status === null) return PHASE_TEXT.idle
  if (status.phase === 'buffering') return `${PHASE_TEXT.buffering} ${bufferedPct}%`
  return PHASE_TEXT[status.phase]
}

/** 读屏播报用的粗粒度阶段：不含每秒都在变的百分比 */
function coarsePhase(state: ActivePlayState, status: TorrentStatus | null): string {
  if (state.phase === 'streaming') return '正在边下边播'
  if (state.phase === 'selecting') return '请选择要播放的剧集'
  return status === null ? PHASE_TEXT.idle : PHASE_TEXT[status.phase]
}

/** 标题优先用后端已选中的文件名（比搜索结果标题更贴近实际播放的东西） */
function displayTitle(state: ActivePlayState, status: TorrentStatus | null): string {
  if (state.phase === 'streaming') return state.title
  const fileName = status?.fileName ?? ''
  return fileName === '' ? state.title : fileName
}

/**
 * 分享者与速度读数。peers 为 0 时整块转警示色 —— 这是用户判断
 * 「该不该换一条资源」的唯一依据，不能藏进 tooltip。
 */
function PeerReadout({
  status,
  streaming,
  progressPct,
}: {
  status: TorrentStatus | null
  streaming: boolean
  progressPct: number
}) {
  if (status === null) {
    return (
      <span className="tsb-readout result--dim" style={mono}>
        状态未知
      </span>
    )
  }
  const noPeers = status.peers === 0
  return (
    <span className={noPeers ? 'tsb-readout tsb-readout--nopeers' : 'tsb-readout'} style={mono}>
      <span>
        分享者 {status.peers}
        <span className="tsb-seeders">（做种 {status.seeders}）</span>
      </span>
      <span className="tsb-sep" aria-hidden="true">
        ·
      </span>
      <span>↓ {formatRate(status.downRate)}</span>
      {streaming && (
        <>
          <span className="tsb-sep" aria-hidden="true">
            ·
          </span>
          <span>已下载 {progressPct}%</span>
        </>
      )}
    </span>
  )
}
