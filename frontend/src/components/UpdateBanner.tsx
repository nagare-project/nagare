import { useState } from 'react'
import type { UpdateView } from '../lib/endpoints'
import { formatVersion } from '../lib/format'
import { hudPalette } from '../lib/palette'
import { isHttpUrl } from '../lib/url'
import { label } from '../tokens'
import './update-banner.css'

/** 「忽略此版本」记在 localStorage 里的键；值是被忽略的 latest 原文 */
export const IGNORED_VERSION_KEY = 'nagare.update.ignoredVersion'

export interface UpdateBannerProps {
  /** 根布局传入的更新视图；尚未加载 / 加载失败时为 null（不出提示条） */
  view: UpdateView | null
}

/**
 * 页面顶部的新版本提示条（三个页面共用，由 RootLayout 挂在 Outlet 之上）。
 * 只提示不自更新：给下载外链，「忽略此版本」后同一版本不再提示（换了新版本会再出现）。
 */
export function UpdateBanner({ view }: UpdateBannerProps) {
  const [ignored, setIgnored] = useState<string | null>(readIgnored)

  if (view === null || !view.available || view.latest === '' || ignored === view.latest) {
    return null
  }
  const { latest } = view

  function handleIgnore(): void {
    writeIgnored(latest)
    setIgnored(latest)
  }

  return (
    <aside className="update-banner" style={hudPalette} role="status" aria-live="polite">
      <div className="update-banner-inner">
        <span className="update-banner-tag" style={label}>
          update
        </span>
        <span className="update-banner-text">
          新版本 {formatVersion(view.latest)} 可用（当前 {formatVersion(view.current)}）
        </span>
        {isHttpUrl(view.url) && (
          <a
            className="hud-link update-banner-link"
            href={view.url}
            target="_blank"
            rel="noreferrer noopener"
          >
            下载 ↗
          </a>
        )}
        <button
          type="button"
          className="hud-button hud-button--small hud-button--ghost update-banner-ignore"
          onClick={handleIgnore}
        >
          忽略此版本
        </button>
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
