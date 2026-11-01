import type { CSSProperties } from 'react'
import type { PosterTones } from './endpoints'

/** 一个合法的 #rrggbb：服务端下发的是最终 sRGB 值，其余形状一律不信 */
const HEX_COLOR = /^#[0-9a-f]{6}$/i

/**
 * 海报色阶 → 行内样式（DESIGN.md「海报色」）。
 *
 * 服务端已经按封面色相算好并做完色域映射，这里只把五档颜色原样写成 CSS 自定义属性，
 * 由调用方挂在【使用它的元素自身】（作品页的根、卡片根）—— 不在 CSS 里从色相组装，
 * 「祖先上组装、子元素改色相无效」的陷阱在结构上就不会发生。
 *
 * 没有色阶（null / 缺省）或任何一档不是 #rrggbb 时返回 undefined：不写行内样式，
 * 元素继承 tokens.css 在 :root 上的中性值。只坏一档也整组不用 —— 半套海报色比中性色更难看。
 */
export function posterToneStyle(tones: PosterTones | null | undefined): CSSProperties | undefined {
  if (tones == null) return undefined
  const { tone, quiet, mid, low, onTone } = tones
  if (![tone, quiet, mid, low, onTone].every((value) => typeof value === 'string' && HEX_COLOR.test(value))) return undefined
  return {
    '--poster-tone': tone,
    '--poster-tone-quiet': quiet,
    '--poster-tone-mid': mid,
    '--poster-tone-low': low,
    '--poster-on-tone': onTone,
  }
}
