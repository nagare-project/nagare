import { describe, expect, test } from 'vitest'
import { cardToneVars, oklchCss, pageToneVars, toneLadder } from './tone'

// 发现页的每张卡片、连同整页底色，都从一张海报的色相上色。只有明度与彩度写死，色阶才安全 ——
// 下面的证明走遍 360 个色相（加上没有色相的中性情况），对发现页上每一种「字写在什么底上」逐个算对比度。
// 颜色按 CSS oklch() 的同一种算法换成 sRGB（越界的通道夹到边上），算的就是浏览器画出来的那个颜色。

describe('oklchCss', () => {
  test('写成浏览器认的 oklch()', () => {
    expect(oklchCss(76, 0.085, 222.7)).toBe('oklch(76% 0.085 222.7)')
  })

  test('透明度用斜杠接在后面', () => {
    expect(oklchCss(13, 0.012, 10, 0.78)).toBe('oklch(13% 0.012 10 / 0.78)')
  })

  test('没有色相就没有彩度 —— 中性灰，不是红', () => {
    expect(oklchCss(76, 0.085, null)).toBe('oklch(76% 0 0)')
  })
})

describe('toneLadder', () => {
  test('与 DESIGN.md 的海报色阶一档不差', () => {
    expect(toneLadder(145)).toEqual({
      text: 'oklch(76% 0.085 145)',
      quiet: 'oklch(68% 0.07 145)',
      line: 'oklch(52% 0.07 145)',
      fill: 'oklch(24% 0.045 145)',
      onSolid: 'oklch(15% 0.03 145)',
    })
  })

  test('没有色相时是中性的色阶', () => {
    const t = toneLadder(null)
    expect(t.text).toBe('oklch(76% 0 0)')
    expect(t.fill).toBe('oklch(24% 0 0)')
  })
})

describe('写到元素上的自定义属性', () => {
  test('卡片的变量名就是样式表读的那几个', () => {
    expect(cardToneVars(200)).toEqual({
      '--tone': 'oklch(76% 0.085 200)',
      '--tone-quiet': 'oklch(68% 0.07 200)',
      '--tone-line': 'oklch(52% 0.07 200)',
      '--tone-fill': 'oklch(24% 0.045 200)',
    })
  })

  test('整页的变量多了带色相的底色、面、浮层三层', () => {
    const vars = pageToneVars(30)
    expect(vars['--home-ground']).toBe('oklch(13% 0.012 30)')
    expect(vars['--home-surface']).toBe('oklch(18% 0.014 30)')
    expect(vars['--home-pop']).toBe('oklch(20% 0.016 30)')
    expect(vars['--home-tone']).toBe('oklch(76% 0.085 30)')
    expect(vars['--home-tone-line']).toBe('oklch(52% 0.07 30)')
    expect(vars['--home-on-solid']).toBe('oklch(15% 0.03 30)')
  })
})

// ── 对比度证明 ──────────────────────────────────────────────────────────────

interface Rgb { r: number; g: number; b: number }

const toLinear = (c: number) => { const n = c / 255; return n <= 0.04045 ? n / 12.92 : Math.pow((n + 0.055) / 1.055, 2.4) }
const toSrgb8 = (c: number) => { const v = c <= 0.0031308 ? c * 12.92 : 1.055 * Math.pow(c, 1 / 2.4) - 0.055; return Math.max(0, Math.min(255, Math.round(v * 255))) }

/** OKLCH（L 百分比、色相角度）换成 8 位 sRGB，越界通道夹到边上 */
function rgbOf(L: number, C: number, hue: number | null): Rgb {
  const h = ((hue ?? 0) * Math.PI) / 180
  const c = hue === null ? 0 : C
  const l0 = L / 100
  const a = c * Math.cos(h)
  const b = c * Math.sin(h)
  const l = (l0 + 0.3963377774 * a + 0.2158037573 * b) ** 3
  const m = (l0 - 0.1055613458 * a - 0.0638541728 * b) ** 3
  const s = (l0 - 0.0894841775 * a - 1.291485548 * b) ** 3
  return {
    r: toSrgb8(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    g: toSrgb8(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    b: toSrgb8(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
  }
}

const luminance = ({ r, g, b }: Rgb) => 0.2126 * toLinear(r) + 0.7152 * toLinear(g) + 0.0722 * toLinear(b)
function contrast(x: Rgb, y: Rgb): number {
  const a = luminance(x)
  const b = luminance(y)
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
}

/** `rgba(235,235,245,alpha)` 叠在不透明的底上 */
function over(bg: Rgb, alpha: number): Rgb {
  const mix = (fg: number, b: number) => Math.round(fg * alpha + b * (1 - alpha))
  return { r: mix(235, bg.r), g: mix(235, bg.g), b: mix(245, bg.b) }
}

/** 白色薄纱（rgba(255,255,255,alpha)）盖在不透明的底上 */
function veil(bg: Rgb, alpha: number): Rgb {
  const mix = (b: number) => Math.round(255 * alpha + b * (1 - alpha))
  return { r: mix(bg.r), g: mix(bg.g), b: mix(bg.b) }
}

const HUES: Array<number | null> = [null, ...Array.from({ length: 360 }, (_, i) => i)]

/** 全部色相里最低的对比度 */
function worst(fn: (hue: number | null) => number): number {
  return Math.min(...HUES.map(fn))
}

const AA = 4.5

describe('发现页每一种「字 × 底」在每个色相上都 ≥ 4.5:1', () => {
  const surfaces = [
    ['底色（13%）', 13, 0.012],
    ['面（18%）', 18, 0.014],
    ['浮层（20%）', 20, 0.016],
  ] as const

  for (const [name, L, C] of surfaces) {
    test(`作品色文字写在${name}上`, () => {
      expect(worst((h) => contrast(rgbOf(76, 0.085, h), rgbOf(L, C, h)))).toBeGreaterThanOrEqual(AA)
    })

    test(`压低一档的作品色文字写在${name}上`, () => {
      expect(worst((h) => contrast(rgbOf(68, 0.07, h), rgbOf(L, C, h)))).toBeGreaterThanOrEqual(AA)
    })

    test(`最安静的中性字（58% 白）写在${name}上`, () => {
      expect(worst((h) => { const bg = rgbOf(L, C, h); return contrast(over(bg, 0.58), bg) })).toBeGreaterThanOrEqual(AA)
    })
  }

  test('作品色文字写在它自己的填充上（评分胶囊、类型胶囊、选中的色调胶囊）', () => {
    expect(worst((h) => contrast(rgbOf(76, 0.085, h), rgbOf(24, 0.045, h)))).toBeGreaterThanOrEqual(AA)
  })

  test('实心按钮上的深色字', () => {
    expect(worst((h) => contrast(rgbOf(15, 0.03, h), rgbOf(76, 0.085, h)))).toBeGreaterThanOrEqual(AA)
  })

  test('色调胶囊没选中时：80% 的中性字写在 5% 白纱上', () => {
    expect(worst((h) => { const bg = veil(rgbOf(13, 0.012, h), 0.05); return contrast(over(bg, 0.8), bg) })).toBeGreaterThanOrEqual(AA)
  })

  test('年度榜悬停的一行：作品色名次写在 4.5% 白纱上', () => {
    expect(worst((h) => contrast(rgbOf(76, 0.085, h), veil(rgbOf(13, 0.012, h), 0.045)))).toBeGreaterThanOrEqual(AA)
  })

  test('「下一组」链接是另一组的颜色写在这一组的面上：任意两个色相都 ≥ 4.5:1', () => {
    let low = Infinity
    for (let a = 0; a < 360; a += 5) for (let b = 0; b < 360; b += 5) low = Math.min(low, contrast(rgbOf(76, 0.085, a), rgbOf(18, 0.014, b)))
    expect(low).toBeGreaterThanOrEqual(AA)
  })
})
