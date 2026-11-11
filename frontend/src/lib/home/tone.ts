// 发现页每部作品的颜色，写成 CSS 字符串。
//
// 与作品页的海报色阶同一个思路（DESIGN.md「海报色」）：只有色相角来自封面，明度与彩度在这里写死，
// 所以无论封面是什么颜色，每一档都看得清。色相由服务端从目录的封面主色算好（接口字段 hue，null = 没有）。
//
// 不同的是字符串在哪里拼：发现页同时画几十种色相，在一个元素上声明 --poster-hue、在另一个元素上
// 用它组装颜色，正是 DESIGN.md 写的「静默失效」陷阱（每张卡都成了同一个颜色）。所以最终的 oklch()
// 字符串在这里拼好，直接写到使用它的那个元素自己身上，从结构上杜绝这个错误。
//
// 纯函数：不碰 React、不碰 DOM。

/**
 * `oklch(L% C h)`；没有色相时彩度为 0。
 *
 * 0° 是红色，「没有色相」不能当成「色相 0」—— 而是同一明度的中性灰，色阶的对比度保证照样成立。
 */
export function oklchCss(lightness: number, chroma: number, hue: number | null, alpha?: number): string {
  const body = hue == null ? `${lightness}% 0 0` : `${lightness}% ${chroma} ${hue}`
  return alpha === undefined ? `oklch(${body})` : `oklch(${body} / ${alpha})`
}

export interface ToneLadder {
  /** 76%：作品的颜色用作文字、图标、强调 */
  text: string
  /** 68%：仍能写字，压低一档；色阶里最后一档能写字的颜色 */
  quiet: string
  /** 52%：只给线条（非文字） */
  line: string
  /** 24%：只给填充与胶囊底 */
  fill: string
  /** 15%：压在 text 色实心按钮上的字 */
  onSolid: string
}

export function toneLadder(hue: number | null): ToneLadder {
  return {
    text: oklchCss(76, 0.085, hue),
    quiet: oklchCss(68, 0.07, hue),
    line: oklchCss(52, 0.07, hue),
    fill: oklchCss(24, 0.045, hue),
    onSolid: oklchCss(15, 0.03, hue),
  }
}

/** 写在卡片自己的元素上；后代读 var(--tone) 等 */
export interface CardToneVars {
  '--tone': string
  '--tone-quiet': string
  '--tone-line': string
  '--tone-fill': string
}

export function cardToneVars(hue: number | null): CardToneVars {
  const t = toneLadder(hue)
  return { '--tone': t.text, '--tone-quiet': t.quiet, '--tone-line': t.line, '--tone-fill': t.fill }
}

/**
 * 整页的颜色，跟着横幅当前那一部走。
 *
 * 底色 / 面 / 浮层三层是带一丝色相的近黑（彩度 ≤ 0.016），读起来是「这部作品的夜色」，不是彩色背景。
 * tone.test.ts 逐个色相证明三层上的文字都 ≥ 4.5:1。
 */
export interface PageToneVars {
  '--home-ground': string
  '--home-surface': string
  '--home-pop': string
  '--home-tone': string
  '--home-tone-line': string
  '--home-tone-fill': string
  '--home-on-solid': string
}

export function pageToneVars(hue: number | null): PageToneVars {
  const t = toneLadder(hue)
  return {
    '--home-ground': oklchCss(13, 0.012, hue),
    '--home-surface': oklchCss(18, 0.014, hue),
    '--home-pop': oklchCss(20, 0.016, hue),
    '--home-tone': t.text,
    '--home-tone-line': t.line,
    '--home-tone-fill': t.fill,
    '--home-on-solid': t.onSolid,
  }
}
