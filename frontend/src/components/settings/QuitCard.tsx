import { useState } from 'react'
import { shutdownNagare } from '../../lib/endpoints'
import type { Platform } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { hudPalette } from '../../lib/palette'
import { label, mono } from '../../tokens'
import './cards.css'

export interface QuitCardProps {
  /** 未知（设置尚未加载）时用通用措辞 */
  platform?: Platform
  /** 后端已确认退出；由页面把整页换成 QuitNotice */
  onQuit: () => void
}

/** 退出流程：空闲 → 内联二次确认 → 请求中 → 失败（成功交给 onQuit） */
type QuitState =
  | { phase: 'idle' }
  | { phase: 'confirming' }
  | { phase: 'busy' }
  | { phase: 'error'; message: string }

/** 各平台除了本按钮之外的退出方式；Linux 没托盘，全靠这里 */
const PLATFORM_NOTE: Record<Platform, string> = {
  darwin: 'macOS 也可以从菜单栏图标退出。',
  windows: 'Windows 也可以从托盘图标退出。',
  linux: 'Linux 没有托盘图标，退出请用这里。',
}

const GENERIC_NOTE = 'macOS / Windows 也可以从菜单栏 / 托盘图标退出；Linux 没有托盘，退出请用这里。'

/**
 * 「退出 nagare」卡：关闭浏览器标签页不会结束后台进程，需要显式退出。
 * 二次确认做成内联（不用 window.confirm），退出成功后页面整体替换成 QuitNotice。
 */
export function QuitCard({ platform, onQuit }: QuitCardProps) {
  const [state, setState] = useState<QuitState>({ phase: 'idle' })
  const busy = state.phase === 'busy'

  async function handleConfirm(): Promise<void> {
    setState({ phase: 'busy' })
    try {
      await shutdownNagare()
      onQuit()
    } catch (err) {
      console.error('退出 nagare 失败', err)
      setState({
        phase: 'error',
        message: `${errorText(err, '退出失败')}（如果 nagare 其实已经退出，直接关闭此页即可）`,
      })
    }
  }

  return (
    <section className="panel settings-card" aria-labelledby="quit-heading">
      <h2 id="quit-heading" className="panel-heading" style={label}>
        quit
      </h2>
      <p className="page-notice-copy">
        关闭浏览器标签页不会结束 nagare 的后台进程。
        {platform === undefined ? GENERIC_NOTE : PLATFORM_NOTE[platform]}
      </p>

      {state.phase === 'confirming' || state.phase === 'busy' ? (
        <div className="quit-confirm" role="group" aria-label="确认退出">
          <p className="quit-confirm-copy">确定退出 nagare 吗？正在播放的 mpv 也会一起关闭。</p>
          <div className="form-actions">
            <button
              type="button"
              className="hud-button hud-button--small quit-confirm-button"
              onClick={() => void handleConfirm()}
              disabled={busy}
            >
              {busy ? '正在退出 …' : '确认退出'}
            </button>
            <button
              type="button"
              className="hud-button hud-button--small hud-button--ghost"
              onClick={() => setState({ phase: 'idle' })}
              disabled={busy}
            >
              取消
            </button>
          </div>
        </div>
      ) : (
        <div className="form-actions">
          <button
            type="button"
            className="hud-button hud-button--small hud-button--ghost quit-button"
            onClick={() => setState({ phase: 'confirming' })}
          >
            退出 nagare
          </button>
        </div>
      )}

      <p
        className={state.phase === 'error' ? 'result result--err' : 'result'}
        style={mono}
        role="status"
        aria-live="polite"
      >
        {state.phase === 'error' ? state.message : ''}
      </p>
    </section>
  )
}

/** 退出成功后的整页提示：后端已经不在了，页面上再没有任何可用的动作 */
export function QuitNotice() {
  return (
    <main className="lib-shell" style={hudPalette}>
      <div className="page-notice">
        <p className="panel-heading" style={label}>
          nagare · quit
        </p>
        <h1 className="page-notice-title">nagare 已退出</h1>
        <p className="page-notice-copy">
          后台进程已经结束，可以关闭此页。下次从应用图标或命令行重新启动 nagare 即可。
        </p>
      </div>
    </main>
  )
}
