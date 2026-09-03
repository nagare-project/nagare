import type { SelfUpdateState } from '../hooks/useSelfUpdate'
import type { SelfUpdateChannel } from './endpoints'
import { formatVersion } from './format'

/** 与 global.css 的 .result--* 调性一一对应 */
export type SelfUpdateTone = 'dim' | 'ok' | 'warn' | 'err'

export interface SelfUpdateStatus {
  tone: SelfUpdateTone
  /** 主文案，**不含**每秒都在变的秒数 —— 可以安全地放进 live region */
  text: string
  /**
   * 已用秒数后缀（`（已 42 秒）`），不在计时的阶段为空串。
   * 每秒都变，读屏会一秒念一遍 —— 调用方要么塞进 aria-hidden，
   * 要么放在非 live 的区域（照 TorrentStatusBar 的处理）。
   */
  elapsed: string
}

/**
 * 一键更新各阶段的中文说明。顶部提示条与设置页的更新卡共用这一份：
 * 同一个流程在两处必须说同一句话，否则用户会以为是两回事。
 * 返回 null 表示当前没有更新在进行（界面照常显示原本的内容）。
 */
export function selfUpdateStatus(
  state: SelfUpdateState,
  elapsedSec: number,
): SelfUpdateStatus | null {
  const elapsed = `（已 ${elapsedSec} 秒）`
  switch (state.phase) {
    case 'idle':
      return null
    case 'applying':
      return { tone: 'dim', text: '正在下载并校验更新包，请不要关闭 nagare …', elapsed }
    case 'restarting':
      return {
        tone: 'dim',
        text: `${formatVersion(state.version)} 已装好，正在等待服务重新启动 …`,
        elapsed,
      }
    case 'done':
      return {
        tone: 'ok',
        text: `已更新到 ${formatVersion(state.version)}，正在刷新页面 …`,
        elapsed: '',
      }
    // 装好了，只是没等到服务回来 —— 与 error 分开措辞，别把一次成功的更新说成失败
    case 'timeout':
      return {
        tone: 'warn',
        text: `${formatVersion(state.version)} 已经装好了，但没等到服务重新启动。请手动重新打开 nagare。`,
        elapsed: '',
      }
    case 'error':
      return { tone: 'err', text: state.message, elapsed: '' }
  }
}

/** 安装方式的中文名（设置页的「自更新」一行） */
export const CHANNEL_LABEL: Record<SelfUpdateChannel, string> = {
  'app-bundle': 'macOS 应用包',
  direct: '安装包 / 便携版',
  package: '系统包管理器',
  unknown: '未知安装方式',
}

/**
 * 后端没给 reason 时按安装方式兜底。
 * 「不能自更新」如果只表现为按钮消失，用户只会以为界面坏了 ——
 * 任何一条降级路径都要说清楚「为什么」和「那我该做什么」。
 */
export const UNSUPPORTED_REASON: Record<SelfUpdateChannel, string> = {
  'app-bundle': '这个安装暂时不能一键更新，请到下载页取新版本。',
  direct: '这个安装暂时不能一键更新，请到下载页取新版本。',
  package: 'nagare 是用系统包管理器装的，更新交给包管理器：Linux 用 apt / dnf 升级，macOS 用 brew upgrade。',
  unknown: '认不出 nagare 是怎么安装的，不能一键更新。请到下载页取新版本，或用当初的安装方式升级。',
}
