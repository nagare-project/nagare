import { hintText } from '../../components/BackgroundHint'
import { inDesktopWindow } from '../../lib/desktopWindow'
import type { BackgroundMode, Platform } from '../../lib/endpoints'

/** 后台形态的叫法（与顶部那条提示同一套措辞） */
const MODE_LABEL: Record<BackgroundMode, string> = {
  dock: 'Dock 图标与菜单栏图标',
  menubar: '菜单栏图标',
  tray: '系统托盘图标',
  window: 'nagare 自己的窗口',
  none: '没有图标（命令行或无托盘模式）',
}

export interface BackgroundPanelProps {
  mode: BackgroundMode
  platform: Platform
  /** 页面开在 nagare 的桌面窗口里；缺省按 User-Agent 判断 */
  inWindow?: boolean
}

/**
 * 「后台运行」：顶部那条「关掉标签页不会退出」的提示点过「知道了」就不再出现，
 * 这里常驻同一段说明，回头想找「它到底在哪、怎么退出」时有地方看。
 */
export function BackgroundPanel({ mode, platform, inWindow = inDesktopWindow() }: BackgroundPanelProps) {
  return (
    <section className="info-panel" aria-labelledby="background-heading">
      <h3 id="background-heading" className="info-panel-title">
        后台运行
      </h3>
      <dl className="kv-list">
        <dt>在哪里找它</dt>
        <dd>{MODE_LABEL[mode]}</dd>
      </dl>
      <p className="info-panel-text">{hintText(mode, platform, inWindow)}</p>
    </section>
  )
}
