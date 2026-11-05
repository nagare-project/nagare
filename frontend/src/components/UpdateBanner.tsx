import { useEffect, useRef, useState } from 'react'
import { isSettledPhase } from '../hooks/useSelfUpdate'
import type { UseSelfUpdateResult } from '../hooks/useSelfUpdate'
import type { UpdateView } from '../lib/endpoints'
import { handOffFocus, pageHeading } from '../lib/focus'
import { formatVersion } from '../lib/format'
import { selfUpdateStatus } from '../lib/selfUpdateText'
import { isHttpUrl } from '../lib/url'
import { Icon } from './ui/Icon'
import './app-banner.css'

/** 「忽略此版本」记在 localStorage 里的键；值是被忽略的 latest 原文 */
export const IGNORED_VERSION_KEY = 'nagare.update.ignoredVersion'

export interface UpdateBannerProps {
  /** 根布局传入的更新视图；尚未加载 / 加载失败时为 null（不出提示条） */
  view: UpdateView | null
  /** 一键更新流程（与设置页的更新卡是同一份，见 RootLayout） */
  selfUpdate: UseSelfUpdateResult
}

/**
 * 页面顶部的新版本提示条（三个页面共用，由 RootLayout 挂在 Outlet 之上）。
 * 给下载外链；能自更新时再给一个「立即更新」，更新期间就地显示阶段文案。
 * 「忽略此版本」后同一版本不再提示（换了新版本会再出现）。
 *
 * 更新已经装好（done / timeout）之后不再给「立即更新」——按钮会把刚装好的版本
 * 重下一遍，与旁边「已经装好了」的文案自相矛盾（见 isSettledPhase）。
 */
export function UpdateBanner({ view, selfUpdate }: UpdateBannerProps) {
  const [ignored, setIgnored] = useState<string | null>(readIgnored)
  // 点「忽略此版本」后提示条连同按钮一起消失：焦点交给页面主标题，不掉到 <body> 上
  const handOff = useRef(false)
  useEffect(() => {
    if (ignored === null || !handOff.current) return
    handOff.current = false
    handOffFocus(pageHeading())
  }, [ignored])

  if (view === null || !view.available || view.latest === '' || ignored === view.latest) {
    return null
  }
  const { latest } = view
  const status = selfUpdateStatus(selfUpdate.state, selfUpdate.elapsedSec)
  const failed = selfUpdate.state.phase === 'error'
  const settled = isSettledPhase(selfUpdate.state.phase)

  function handleIgnore(): void {
    writeIgnored(latest)
    handOff.current = true
    setIgnored(latest)
  }

  return (
    <aside className="app-banner update-banner" role="status" aria-live="polite">
      <div className="app-banner-inner">
        <span className="app-banner-icon" aria-hidden="true">
          <Icon name="refresh" size={16} />
        </span>
        {selfUpdate.busy && <span className="update-banner-dot" aria-hidden="true" />}
        {/* 整条 aside 是 live region，所以每秒都在变的秒数必须 aria-hidden，
            否则读屏会一秒念一遍（与 TorrentStatusBar 的处理同一手法） */}
        <span className={status === null ? 'app-banner-text' : `app-banner-text result--${status.tone}`}>
          {status === null ? (
            <>
              新版本 {formatVersion(view.latest)} 可用（当前 {formatVersion(view.current)}）
            </>
          ) : (
            <>
              {status.text}
              <span aria-hidden="true">{status.elapsed}</span>
            </>
          )}
        </span>
        <span className="app-banner-actions">
          {isHttpUrl(view.url) && (
            <a className="link" href={view.url} target="_blank" rel="noreferrer noopener">
              下载 ↗
            </a>
          )}
          {view.selfUpdate.supported && !settled && (
            <button type="button" className="btn btn--sm" onClick={selfUpdate.start} disabled={selfUpdate.busy}>
              {selfUpdate.busy ? '更新中 …' : failed ? '重试' : '立即更新'}
            </button>
          )}
          {/* 更新期间不给「忽略此版本」：点了提示条连同进度一起消失，而后端还在
              下载/替换 —— 用户会以为自己取消掉了。换成一句说明，别留个哑掉的控件。 */}
          {selfUpdate.busy ? (
            <span className="app-banner-note">更新进行中，暂时不能忽略此版本</span>
          ) : (
            <button type="button" className="btn btn--sm app-banner-quiet" onClick={handleIgnore}>
              忽略此版本
            </button>
          )}
        </span>
      </div>
    </aside>
  )
}

/** localStorage 在隐私模式 / 禁用站点数据时会抛，读写都包起来，失败当作没记 */
function readIgnored(): string | null {
  try {
    return window.localStorage.getItem(IGNORED_VERSION_KEY)
  } catch (err) {
    console.error('读取「忽略的版本」失败', err)
    return null
  }
}

function writeIgnored(version: string): void {
  try {
    window.localStorage.setItem(IGNORED_VERSION_KEY, version)
  } catch (err) {
    console.error('记录「忽略的版本」失败', err)
  }
}
