import { useState } from 'react'
import { inDesktopWindow } from '../lib/desktopWindow'
import type { BackgroundMode, Platform } from '../lib/endpoints'
import { Icon } from './ui/Icon'
import './background-hint.css'

/** 「知道了」记在 localStorage 里的键；值固定为 '1' */
export const BACKGROUND_HINT_DISMISSED_KEY = 'nagare.backgroundHint.dismissed'

export interface BackgroundHintProps {
  /** 后台形态；设置尚未加载时传 null（不出提示条） */
  mode: BackgroundMode | null
  platform: Platform | null
  /** 页面开在 nagare 的桌面窗口里（不是外部浏览器）；缺省按 User-Agent 判断 */
  inWindow?: boolean
}

/**
 * 页面顶部的「关掉标签页不会退出 nagare」提示条（三层提示里的界面层，
 * 另两层是 Dock / 托盘图标本身与首次启动的系统通知）。在桌面窗口里说的是「关闭窗口」。
 *
 * 只出现一次：点「知道了」之后不再显示。文案按后台形态写清「去哪里找它、怎么退出」，
 * 不能写死「菜单栏」—— Windows 用户看到会去找一个不存在的东西。
 */
export function BackgroundHint({ mode, platform, inWindow = inDesktopWindow() }: BackgroundHintProps) {
  const [dismissed, setDismissed] = useState<boolean>(readDismissed)

  if (dismissed || mode === null) {
    return null
  }

  function handleDismiss(): void {
    writeDismissed()
    setDismissed(true)
  }

  return (
    <aside className="background-hint" role="note">
      <div className="background-hint-inner">
        <span className="background-hint-icon" aria-hidden="true">
          <Icon name="info" size={16} />
        </span>
        <span className="background-hint-text">{hintText(mode, platform, inWindow)}</span>
        <button type="button" className="btn btn--sm background-hint-dismiss" onClick={handleDismiss}>
          知道了
        </button>
      </div>
    </aside>
  )
}

/**
 * 按形态措辞；平台区分 Linux 托盘可能整个不显示、桌面窗口在 macOS 与 Windows 上去哪里找。
 * inWindow：页面开在 nagare 的桌面窗口里 —— 只在 window 形态下有意义（别的形态没有窗口）。
 */
export function hintText(mode: BackgroundMode, platform: Platform | null, inWindow = false): string {
  if (mode === 'window') {
    return inWindow ? windowHintText(platform) : windowTabHintText(platform)
  }
  const lead = '关闭这个标签页不会停止 nagare，它在后台继续运行。'
  switch (mode) {
    case 'dock':
      return `${lead}点 Dock 图标可以重新打开界面，⌘Q 或菜单栏图标可以退出（会先回写观看进度）。`
    case 'menubar':
      return `${lead}从菜单栏右上角的「流」图标可以重新打开界面或退出。`
    case 'tray':
      if (platform === 'linux') {
        return `${lead}从系统托盘图标可以重新打开界面或退出；桌面不显示托盘时，用设置页里的「退出 nagare」。`
      }
      return `${lead}从系统托盘图标（可能折叠在任务栏的「^」里）可以重新打开界面或退出。`
    default:
      return `${lead}退出请用设置页里的「退出 nagare」，或在启动它的终端按 Ctrl+C。`
  }
}

/**
 * 桌面窗口里：关窗口只是把它藏起来，后台的下载照旧。不说「做种」—— 关窗口会停播，边下边播的种子随之停下
 * （决议 M3-2：停播即停），下完的下载也不做种。macOS 从 Dock / 菜单栏「流」回来，⌘Q 退出。
 */
function windowHintText(platform: Platform | null): string {
  if (platform === 'windows') {
    return '关闭窗口不会退出 nagare，正在进行的下载会继续。从系统托盘图标（可能折叠在任务栏的「^」里）可以重新打开窗口或退出。'
  }
  return '关闭窗口不会退出 nagare，正在进行的下载会继续。点 Dock 图标或菜单栏的「流」可以重新打开，按 ⌘Q 退出。'
}

/**
 * 有桌面窗口、这一页却开在外部浏览器里（托盘「在浏览器中打开」）：关的是标签页，回去找的是 nagare 的窗口。
 * 不提 ⌘Q —— 在浏览器里按，退出的是浏览器。
 */
function windowTabHintText(platform: Platform | null): string {
  const lead = '关闭这个标签页不会停止 nagare，它在后台继续运行。'
  if (platform === 'windows') {
    return `${lead}从系统托盘图标（可能折叠在任务栏的「^」里）可以回到 nagare 的窗口或退出。`
  }
  return `${lead}点 Dock 图标或菜单栏的「流」可以回到 nagare 的窗口，退出也在「流」的菜单里。`
}

/** localStorage 在隐私模式 / 禁用站点数据时会抛，读写都包起来，失败当作没记 */
function readDismissed(): boolean {
  try {
    return window.localStorage.getItem(BACKGROUND_HINT_DISMISSED_KEY) === '1'
  } catch (err) {
    console.error('读取「后台提示已关闭」失败', err)
    return false
  }
}

function writeDismissed(): void {
  try {
    window.localStorage.setItem(BACKGROUND_HINT_DISMISSED_KEY, '1')
  } catch (err) {
    console.error('记录「后台提示已关闭」失败', err)
  }
}
