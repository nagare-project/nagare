import { useState } from 'react'
import type { UseUpdateResult } from '../../hooks/useUpdate'
import type { UpdateView } from '../../lib/endpoints'
import { errorText, formatDateTime, formatVersion } from '../../lib/format'
import { isHttpUrl } from '../../lib/url'
import { label, mono } from '../../tokens'
import './cards.css'

export interface UpdateCardProps {
  update: UseUpdateResult
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
 * 「自动检查更新」开关。只提示不自更新（M4 零证书方案）。
 * 数据来自根布局共享的 useUpdate，点「立即检查」后顶部提示条同步变化。
 */
export function UpdateCard({ update }: UpdateCardProps) {
  const { state } = update
  const [action, setAction] = useState<ActionState>({ phase: 'idle' })
  const busy = action.phase === 'busy'

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
      <h2 id="update-heading" className="panel-heading" style={label}>
        update
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
              className="hud-button hud-button--small"
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
                  className="hud-link update-download"
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
          </dl>
          {state.data.error !== '' && (
            <p className="result result--err update-error" style={mono} role="alert">
              上次检查失败：{state.data.error}
            </p>
          )}

          <div className="form-actions update-actions">
            <button
              type="button"
              className="hud-button hud-button--small"
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
