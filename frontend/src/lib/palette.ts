import type { CSSProperties } from 'react'
import { HUE, LIBRARY_HUE, PROGRESS_FILL, PROGRESS_TRACK, oklchToken } from '../tokens'

/**
 * 全站 HUD 点缀色，全部由 tokens.ts 的层级表生成（LIVE Cyan，HUE.s07 = 195）。
 * 以 CSS 变量注入各页面根元素；结构性样式在 styles/*.css 里引用这些变量。
 * M0 时该表内联在 HomePage 里，M1 起两个页面共用，抽到这里。
 */
export const hudPalette = {
  '--hud-border': oklchToken('rail', HUE.s07, 0.35),
  '--hud-border-dim': oklchToken('rail', HUE.s07, 0.16),
  '--hud-accent': oklchToken('readout', HUE.s07),
  '--hud-accent-hot': oklchToken('hot', HUE.s07),
  '--hud-flash': oklchToken('flash', HUE.s07, 0.12),

  /* 琥珀：kind 徽标 / 低置信标记，沿用 tokens 里「未归类」章节色（hue 40） */
  '--hud-amber': oklchToken('readout', LIBRARY_HUE.unclassified),
  '--hud-amber-border': oklchToken('rail', LIBRARY_HUE.unclassified, 0.55),

  /* 观看进度条：轨道/填充直接取 tokens 常量，与 animego 播放器视觉一致 */
  '--progress-fill': PROGRESS_FILL,
  '--progress-track': PROGRESS_TRACK,
} satisfies CSSProperties
