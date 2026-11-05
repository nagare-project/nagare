import { useEffect, useRef, useState } from 'react'
import { inDesktopWindow } from '../../lib/desktopWindow'
import { shutdownNagare } from '../../lib/endpoints'
import type { Platform } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { mono } from '../../theme'
import { FullPageNotice } from '../FullPageNotice'
import '../../styles/panels.css'
import './cards.css'

export interface QuitCardProps {
  /** 未知（设置尚未加载）时用通用措辞 */
  platform?: Platform
  /** 页面开在 nagare 的桌面窗口里（不是外部浏览器）；缺省按 User-Agent 判断 */
  inWindow?: boolean
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

/** 卡片上的说明：先说关掉什么不算退出，再说别的退出方式。桌面窗口里关的是窗口，macOS 还有 ⌘Q */
export function quitNote(platform: Platform | undefined, inWindow: boolean): string {
  if (inWindow) {
    const other = platform === 'windows' ? PLATFORM_NOTE.windows : '也可以按 ⌘Q，或从菜单栏的「流」图标退出。'
    return `关闭窗口不会结束 nagare 的后台进程。${other}`
  }
  return `关闭浏览器标签页不会结束 nagare 的后台进程。${platform === undefined ? GENERIC_NOTE : PLATFORM_NOTE[platform]}`
}

/**
 * 「退出 nagare」卡：关闭浏览器标签页（桌面窗口里是关闭窗口）不会结束后台进程，需要显式退出。
 * 二次确认做成内联（不用 window.confirm），退出成功后页面整体替换成 QuitNotice。
 */
export function QuitCard({ platform, inWindow = inDesktopWindow(), onQuit }: QuitCardProps) {
  const [state, setState] = useState<QuitState>({ phase: 'idle' })
  const busy = state.phase === 'busy'
  const card = useRef<HTMLElement>(null)
  const quitButton = useRef<HTMLButtonElement>(null)
  const cancelButton = useRef<HTMLButtonElement>(null)
  // 按下「退出 nagare」后它自己被确认区换掉：焦点不能跟着掉到 <body> 上（与封面缓存卡同一手法）
  const returnFocus = useRef(false)

  useEffect(() => {
    if (state.phase === 'confirming') {
      cancelButton.current?.focus()
      return
    }
    if (state.phase === 'busy' || !returnFocus.current) return
    returnFocus.current = false
    // 退出失败可能要等好几秒才回来：用户这时已经去了别处，就不把焦点抢回来
    const active = document.activeElement
    if (active !== null && active !== document.body && card.current?.contains(active) !== true) return
    quitButton.current?.focus()
  }, [state.phase])

  async function handleConfirm(): Promise<void> {
    setState({ phase: 'busy' })
    try {
      await shutdownNagare()
      onQuit()
    } catch (err) {
      console.error('退出 nagare 失败', err)
      returnFocus.current = true
      setState({
        phase: 'error',
        message: `${errorText(err, '退出失败')}（如果 nagare 其实已经退出，直接关闭此页即可）`,
      })
    }
  }

  return (
    <section ref={card} className="info-panel" aria-labelledby="quit-heading">
      <h3 id="quit-heading" className="info-panel-title">
        退出 nagare
      </h3>
      <p className="info-panel-text">{quitNote(platform, inWindow)}</p>

      {state.phase === 'confirming' || state.phase === 'busy' ? (
        <div className="quit-confirm" role="group" aria-label="确认退出" aria-describedby="quit-confirm-copy">
          <p id="quit-confirm-copy" className="quit-confirm-copy">{inWindow ? '确定退出 nagare 吗？正在播放的视频也会停止。' : '确定退出 nagare 吗？正在播放的 mpv 也会一起关闭。'}</p>
          <div className="form-actions">
            <button type="button" className="btn btn--destructive" onClick={() => void handleConfirm()} disabled={busy}>
              {busy ? '正在退出 …' : '确认退出'}
            </button>
            <button
              ref={cancelButton}
              type="button"
              className="btn"
              onClick={() => {
                returnFocus.current = true
                setState({ phase: 'idle' })
              }}
              disabled={busy}
            >
              取消
            </button>
          </div>
        </div>
      ) : (
        <div className="form-actions">
          <button ref={quitButton} type="button" className="btn btn--danger" onClick={() => setState({ phase: 'confirming' })}>
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
    <FullPageNotice eyebrow="已退出" title="nagare 已退出">
      后台进程已经结束，可以关闭此页。下次从应用图标或命令行重新启动 nagare 即可。
    </FullPageNotice>
  )
}
