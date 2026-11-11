// 把横幅标题拆成一段段依次显现的小块。
//
// 每块渲染成 inline-block，行可以在任意两块之间折开，所以这个函数就是标题的断行规则：
//   - 每个中日韩字自成一块。中文本来就能在任意两个字之间断行，不损失什么，每个字各有一拍；
//   - 一串拉丁字母、数字与词内标点算一块。拆成单个字母的话「SHELL」可能断成「SHE / LL」；
//   - 空白变成一个不断行空格单独成块。inline-block 里单独一个普通空格宽度为零，「THE」「GHOST」会粘在一起。
//
// 纯函数：不碰 DOM、不碰 React。

export const NBSP = ' '

// 拉丁字母（含重音）、十进制数字，以及长在词里的 ASCII 标点：「Re:ZERO」「Dr.STONE」「Rock'n」「Q&A」「Sword-Art」
const TOKEN = /[\p{Script=Latin}\p{Nd}:.'!?&-]+|\s+|./gsu

export function titleTokens(title: string): string[] {
  const trimmed = title.trim()
  if (!trimmed) return []
  const out: string[] = []
  for (const match of trimmed.matchAll(TOKEN)) {
    out.push(/^\s+$/u.test(match[0]) ? NBSP : match[0])
  }
  return out
}

/** 相邻两块相隔（26ms 一拍） */
const STAGGER_MS = 26
/** 切换时：等横幅图动起来之后再开始 */
const SWITCH_START_MS = 160
const SWITCH_CAP_MS = 900
/** 页面第一次画出来时：等横幅自己的进场开始之后 */
const ENTRY_START_MS = 420
const ENTRY_CAP_MS = 1200

export interface TokenDelays {
  /** 这一张切成当前时的过渡延迟 */
  switchMs: number
  /** 页面第一次画出来时的动画延迟 */
  entryMs: number
}

export function tokenDelays(index: number): TokenDelays {
  return {
    switchMs: Math.min(SWITCH_START_MS + index * STAGGER_MS, SWITCH_CAP_MS),
    entryMs: Math.min(ENTRY_START_MS + index * STAGGER_MS, ENTRY_CAP_MS),
  }
}
