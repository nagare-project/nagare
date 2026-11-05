import { describe, expect, it } from 'vitest'
import { posterToneStyle } from './posterTones'

const TONES = { hue: 250.4, tone: '#9fb8e8', quiet: '#8199c8', mid: '#4f6591', low: '#141b2a', onTone: '#05070d' }

describe('posterToneStyle', () => {
  it('五档颜色原样写成 CSS 自定义属性', () => {
    expect(posterToneStyle(TONES)).toEqual({
      '--poster-tone': '#9fb8e8',
      '--poster-tone-quiet': '#8199c8',
      '--poster-tone-mid': '#4f6591',
      '--poster-tone-low': '#141b2a',
      '--poster-on-tone': '#05070d',
    })
  })

  it('没有色阶时不写行内样式，继承 :root 上的中性值', () => {
    expect(posterToneStyle(null)).toBeUndefined()
    expect(posterToneStyle(undefined)).toBeUndefined()
  })

  it('任何一档不是 #rrggbb 就整组不用（不会拼出半套海报色，也不会把奇怪的值写进样式）', () => {
    expect(posterToneStyle({ ...TONES, mid: 'oklch(52% 0.07 250)' })).toBeUndefined()
    expect(posterToneStyle({ ...TONES, tone: 'red;background:url(x)' })).toBeUndefined()
  })
})
