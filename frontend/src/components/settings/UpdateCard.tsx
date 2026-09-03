import { useState } from 'react'
import { isSettledPhase } from '../../hooks/useSelfUpdate'
import type { UseSelfUpdateResult } from '../../hooks/useSelfUpdate'
import type { UseUpdateResult } from '../../hooks/useUpdate'
import type { UpdateView } from '../../lib/endpoints'
import { errorText, formatDateTime, formatVersion } from '../../lib/format'
import { CHANNEL_LABEL, UNSUPPORTED_REASON, selfUpdateStatus } from '../../lib/selfUpdateText'
import type { SelfUpdateStatus } from '../../lib/selfUpdateText'
import { isHttpUrl } from '../../lib/url'
import { mono } from '../../theme'
import './cards.css'

export interface UpdateCardProps {
  update: UseUpdateResult
  /** 一键更新流程；由根布局持有，切页面不会中断（见 useSelfUpdate） */
  selfUpdate: UseSelfUpdateResult
}

type Action = 'check' | 'toggle'

/** 动作结果行状态机 */
type ActionState =
  | { phase: 'idle' }
  | { phase: 'busy'; action: Action }
  | { phase: 'ok'; text: string }
  | { phase: 'error'; message: string }

/**
 * 设置页「更新」卡：当前 / 最新版本、上次检查时间、失败原因、「立即检查」与
 * 「自动检查更新」开关，以及能自更新时的「立即更新」（M4 阶段 B）。
 * 数据来自根布局共享的 useUpdate，点「立即检查」后顶部提示条同步变化。
 */
export function UpdateCard({ update, selfUpdate }: UpdateCardProps) {
  const { state } = update
  const [action, setAction] = useState<ActionState>({ phase: 'idle' })
  // 更新期间连「立即检查」和自动检查开关一起锁上：那时改这些既没意义，
  // 又会让用户以为可以在下载途中改主意。
  const busy = action.phase === 'busy' || selfUpdate.busy

  async function handleCheck(): Promise<void> {
    setAction({ phase: 'busy', action: 'check' })
    try {
      const view = await update.check()
      setAction({ phase: 'ok', text: summarize(view) })
    } catch (err) {
      console.error('检查更新失败', err)
      setAction({ phase: 'error', message: errorText(err, '检查更新失败') })
    }
  }

  async function handleToggle(enabled: boolean): Promise<void> {
    setAction({ phase: 'busy', action: 'toggle' })
    try {
      await update.setEnabled(enabled)
      setAction({ phase: 'ok', text: enabled ? '已开启自动检查' : '已关闭自动检查' })
    } catch (err) {
      console.error('切换自动检查更新失败', err)
      setAction({ phase: 'error', message: errorText(err, '切换自动检查失败') })
    }
  }

  const view = actionView(action)

  return (
    <section className="panel settings-card" aria-labelledby="update-heading">
      <h2 id="update-heading" className="panel-heading">
        更新
      </h2>

      {state.phase === 'loading' && (
        <p className="result result--dim" style={mono} role="status">
          正在读取更新状态 …
        </p>
      )}
      {(state.phase === 'error' || state.phase === 'unauthorized') && (
        <>
          <p className="result result--err" style={mono}>
            {state.phase === 'error' ? state.message : '未携带有效 token，无法读取更新状态'}
          </p>
          <p>
            <button
              type="button"
              className="btn btn--sm"
              onClick={() => void update.reload()}
            >
              重试
            </button>
          </p>
        </>
      )}
      {state.phase === 'ready' && (
        <>
          <dl className="kv-list">
            <dt>当前版本</dt>
            <dd style={mono}>{formatVersion(state.data.current)}</dd>
            <dt>最新版本</dt>
            <dd style={mono}>
              {formatVersion(state.data.latest)}
              {state.data.available && (
                <span className="badge badge--accent update-badge">新版本</span>
              )}
              {state.data.available && isHttpUrl(state.data.url) && (
                <a
                  className="link update-download"
                  href={state.data.url}
                  target="_blank"
                  rel="noreferrer noopener"
                >
                  下载 ↗
                </a>
              )}
            </dd>
            <dt>上次检查</dt>
            <dd style={mono}>
              {state.data.checkedAt === null ? '尚未检查' : formatDateTime(state.data.checkedAt)}
            </dd>
            {/* 能不能一键更新是常驻信息：等到有新版本才发现「原来我这个装法不行」太晚了 */}
            <dt>自更新</dt>
            <dd>
              {state.data.selfUpdate.supported
                ? `可用（${CHANNEL_LABEL[state.data.selfUpdate.channel]}）`
                : `不可用（${CHANNEL_LABEL[state.data.selfUpdate.channel]}）`}
            </dd>
          </dl>
          {state.data.error !== '' && (
            <p className="result result--err update-error" style={mono} role="alert">
              上次检查失败：{state.data.error}
            </p>
          )}

          <SelfUpdateSection view={state.data} selfUpdate={selfUpdate} />

          <div className="form-actions update-actions">
            <button
              type="button"
              className="btn btn--sm"
              onClick={() => void handleCheck()}
              disabled={busy}
            >
              {action.phase === 'busy' && action.action === 'check' ? '检查中 …' : '立即检查'}
            </button>
            <label className="update-toggle">
              <button
                type="button"
                role="switch"
                className="switch"
                aria-checked={state.data.enabled}
                aria-label="自动检查更新"
                onClick={() => void handleToggle(!state.data.enabled)}
                disabled={busy}
              >
                <span className="switch-knob" aria-hidden="true" />
              </button>
              <span className="update-toggle-text">自动检查更新</span>
            </label>
          </div>
          <p className="page-notice-copy update-note">
            开启后每天最多向 GitHub 查询一次最新发布，只发送版本号；发现新版本时页面顶部会提示，
            不会自动下载或替换程序。
          </p>
        </>
      )}

      <p
        className={view === null ? 'result' : `result result--${view.tone}`}
        style={mono}
        role="status"
        aria-live="polite"
      >
        {view?.text}
      </p>
    </section>
  )
}

/**
 * 一键更新区块。四种形态：
 * - 不支持（包管理器装的等）：不给按钮，改为说清原因 + 手动更新的去处；
 * - 支持且有新版本：「立即更新」+ 一行说明它到底会做什么、动哪个文件；
 * - 更新进行中：阶段文案 + 走秒（活着的反馈），按钮禁用；
 * - 已经装好（done / timeout）：**只留收尾文案，不留按钮** —— 见 isSettledPhase，
 *   文案说「已经装好了」而按钮会把同一版本重下一遍，那是界面在自相矛盾。
 *   失败（error）才给「重试」，那时确实什么都没装上。
 *
 * 没有新版本且没有更新在进行时整块不渲染 —— 那时它没有任何可说的。
 */
function SelfUpdateSection({
  view,
  selfUpdate,
}: {
  view: UpdateView
  selfUpdate: UseSelfUpdateResult
}) {
  const { supported, channel, reason, target } = view.selfUpdate
  const status = selfUpdateStatus(selfUpdate.state, selfUpdate.elapsedSec)
  const failed = selfUpdate.state.phase === 'error'
  const downloadable = isHttpUrl(view.url)

  if (!view.available && status === null) return null

  // 新版本已经落盘：done 等着自动刷新，timeout 要用户手动重开。两者都不给按钮。
  if (isSettledPhase(selfUpdate.state.phase) && status !== null) {
    return (
      <div className="self-update">
        {selfUpdate.state.phase === 'timeout' ? (
          // 终态且需要用户动手，用与 mpv 缺失同一条警示样式，别让它淹在正文里。
          // 文案是静态的（不含走秒），挂 role="status" 不会被反复播报。
          <p className="alert-warn self-update-settled" role="status">
            {status.text}
          </p>
        ) : (
          <StatusLine status={status} />
        )}
      </div>
    )
  }

  if (!supported) {
    return (
      <div className="self-update">
        <p className="alert-warn self-update-unsupported">
          {reason === undefined || reason === '' ? UNSUPPORTED_REASON[channel] : reason}
        </p>
        {downloadable && (
          <a
            className="link self-update-link"
            href={view.url}
            target="_blank"
            rel="noreferrer noopener"
          >
            打开下载页手动更新 ↗
          </a>
        )}
        {/* 更新途中后端把 supported 翻成 false 是极小概率，但真发生时
            「正在更新」的状态比「不支持」更该被看见，所以两条都留着 */}
        <StatusLine status={status} />
      </div>
    )
  }

  return (
    <div className="self-update">
      <div className="form-actions self-update-actions">
        {selfUpdate.busy && <span className="self-update-dot" aria-hidden="true" />}
        <button
          type="button"
          className="btn btn--sm"
          onClick={selfUpdate.start}
          disabled={selfUpdate.busy}
        >
          {selfUpdate.busy ? '更新中 …' : failed ? '重试' : '立即更新'}
        </button>
        {failed && downloadable && (
          <a
            className="link self-update-link"
            href={view.url}
            target="_blank"
            rel="noreferrer noopener"
          >
            改用下载页手动更新 ↗
          </a>
        )}
      </div>

      <p className="self-update-note">
        会依次下载新版本、验证 minisign 签名与校验和、替换{' '}
        <code className="self-update-target" style={mono}>
          {target}
        </code>
        ，然后自动重启并刷新页面。下载可能要几分钟，其间请不要关闭 nagare。
      </p>

      <StatusLine status={status} />
    </div>
  )
}

/**
 * 阶段文案。可见那行**不是** live region —— 秒数每秒都变，挂上去读屏会一秒念一遍；
 * 播报交给旁边那个只含粗粒度文案的隐藏 live region（与 TorrentStatusBar 同一手法）。
 */
function StatusLine({ status }: { status: SelfUpdateStatus | null }) {
  return (
    <>
      {status !== null && (
        <p className={`result result--${status.tone} self-update-status`} style={mono}>
          {status.text}
          <span aria-hidden="true">{status.elapsed}</span>
        </p>
      )}
      <span className="visually-hidden" role="status" aria-live="polite">
        {status?.text ?? ''}
      </span>
    </>
  )
}

/** 「立即检查」的一句话结论；GitHub 查询失败时 error 已在上方红字展示，这里只点一下 */
function summarize(view: UpdateView): string {
  if (view.error !== '') return '检查未完成，原因见上方'
  if (view.available) return `发现新版本 ${formatVersion(view.latest)}`
  return `已是最新版本 ${formatVersion(view.current)}`
}

function actionView(state: ActionState): { tone: 'dim' | 'ok' | 'err'; text: string } | null {
  switch (state.phase) {
    case 'idle':
      return null
    case 'busy':
      return { tone: 'dim', text: state.action === 'check' ? '正在向 GitHub 查询 …' : '保存中 …' }
    case 'ok':
      return { tone: 'ok', text: state.text }
    case 'error':
      return { tone: 'err', text: state.message }
  }
}
