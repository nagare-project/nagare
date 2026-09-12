import { describe, expect, it } from 'vitest'
import { seasonOfTitle } from './MediaTorrentButton'

describe('seasonOfTitle', () => {
  it('识别中文、S/Season、罗马数字与序数写法；没写就返回 undefined', () => {
    expect(seasonOfTitle('排球少年')).toBeUndefined()
    expect(seasonOfTitle('排球少年 第二季')).toBe(2)
    expect(seasonOfTitle('关于我转生变成史莱姆这档事 第四季')).toBe(4)
    expect(seasonOfTitle('无职转生 第三季 ～到了异世界就拿出真本事～')).toBe(3)
    expect(seasonOfTitle('葬送的芙莉莲 第2期')).toBe(2)
    expect(seasonOfTitle('Youjo Senki S2')).toBe(2)
    expect(seasonOfTitle('Haikyu!! Season 3')).toBe(3)
    expect(seasonOfTitle('幼女戦記Ⅱ')).toBe(2)
    expect(seasonOfTitle('Sousou no Frieren II')).toBe(2)
    expect(seasonOfTitle('Mushoku Tensei 2nd Season')).toBe(2)
    expect(seasonOfTitle('排球少年！！TO THE TOP')).toBeUndefined()
  })
})
