import type { CSSProperties } from 'react'
import type { PosterTones } from './endpoints'

/** 服务端下发的颜色只认 #rrggbb：别的写法（含 url()、var()）一律当没有，退回中性值 */
const HEX = /^#[0-9a-f]{6}$/i

/**
 * 一部作品的海报色阶 → 写在【使用它的元素自身】上的行内样式（DESIGN.md「海报色」的陷阱一节）：
 * `--poster-tone*` 在声明它的元素上就替换完了，写在更靠上的元素上，子元素再改也改不动。
 * 所以每张卡片、横幅根元素各自带一份；没有色阶（null / 缺省 / 值不合法）时返回 undefined，
 * 由 tokens.css 里 :root 上的中性值接手。
 */
export function posterToneStyle(tones: PosterTones | null | undefined): CSSProperties | undefined {
  if (!tones) return undefined
  const values = [tones.tone, tones.quiet, tones.mid, tones.low, tones.onTone]
  if (!values.every((value) => typeof value === 'string' && HEX.test(value))) return undefined
  return {
    '--poster-tone': tones.tone,
    '--poster-tone-quiet': tones.quiet,
    '--poster-tone-mid': tones.mid,
    '--poster-tone-low': tones.low,
    '--poster-on-tone': tones.onTone,
  } as CSSProperties
}
