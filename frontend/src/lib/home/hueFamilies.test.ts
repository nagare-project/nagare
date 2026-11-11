import { describe, expect, test } from 'vitest'
import { HUE_FAMILIES, colouredFirst, defaultFamily, groupByHueFamily, hueFamilyOf, nextFamily, type HueFamilyKey } from './hueFamilies'

describe('hueFamilyOf', () => {
  const cases: Array<[number, HueFamilyKey]> = [
    [0, 'red'], [20, 'red'], [345, 'red'], [359.9, 'red'], [34.9, 'red'],
    [35, 'orange'], [74.9, 'orange'],
    [75, 'yellow'], [114.9, 'yellow'],
    [115, 'green'], [164.9, 'green'],
    [165, 'cyan'], [214.9, 'cyan'],
    [215, 'blue'], [274.9, 'blue'],
    [275, 'purple'], [344.9, 'purple'],
  ]
  for (const [hue, family] of cases) {
    test(`${hue}° 是 ${family}`, () => {
      expect(hueFamilyOf(hue)).toBe(family)
    })
  }

  test('0–360 之外的角度先规整', () => {
    expect(hueFamilyOf(380)).toBe('red')
    expect(hueFamilyOf(-90)).toBe('blue')
  })

  test('没有色相就不属于任何一组', () => {
    expect(hueFamilyOf(null)).toBeNull()
    expect(hueFamilyOf(Number.NaN)).toBeNull()
  })

  test('七段刚好铺满一圈，不重叠也不留缝', () => {
    for (let tenth = 0; tenth < 3600; tenth++) {
      const hue = tenth / 10
      const hits = HUE_FAMILIES.filter((f) => (f.lo > f.hi ? hue >= f.lo || hue < f.hi : hue >= f.lo && hue < f.hi))
      expect(hits, `${hue}°`).toHaveLength(1)
    }
  })
})

describe('groupByHueFamily', () => {
  const item = (id: number, hue: number | null) => ({ id, hue })

  test('按色相环排组、空组不给、组内保持原顺序', () => {
    const groups = groupByHueFamily([item(1, 250), item(2, 20), item(3, 240), item(4, 100)])
    expect(groups.map((g) => g.key)).toEqual(['red', 'yellow', 'blue'])
    expect(groups.find((g) => g.key === 'blue')?.items.map((i) => i.id)).toEqual([1, 3])
  })

  test('没有色相的作品不进任何一组', () => {
    expect(groupByHueFamily([item(1, null)])).toEqual([])
  })

  test('每组带着自己的代表色相与名字', () => {
    const [green] = groupByHueFamily([item(1, 140)])
    expect(green).toMatchObject({ key: 'green', hue: 145, label: '绿调' })
  })
})

describe('defaultFamily', () => {
  const g = (key: HueFamilyKey, n: number) => ({ key, items: Array.from({ length: n }) })

  test('是最大的那组', () => {
    expect(defaultFamily([g('red', 2), g('blue', 5), g('green', 3)])).toBe('blue')
  })

  test('一样大时按给的顺序，选择是稳定的', () => {
    expect(defaultFamily([g('orange', 3), g('cyan', 3)])).toBe('orange')
  })

  test('没有一组时为 null', () => {
    expect(defaultFamily([])).toBeNull()
  })
})

describe('nextFamily', () => {
  const groups = [{ key: 'red' as const }, { key: 'green' as const }, { key: 'blue' as const }]

  test('是这一组的下一组', () => {
    expect(nextFamily(groups, 'green')).toBe('blue')
  })

  test('最后一组之后回到第一组', () => {
    expect(nextFamily(groups, 'blue')).toBe('red')
  })

  test('只有一组时没有地方可去', () => {
    expect(nextFamily([{ key: 'red' }], 'red')).toBeNull()
  })

  test('不在列表里的组为 null', () => {
    expect(nextFamily(groups, 'purple')).toBeNull()
  })
})

describe('colouredFirst', () => {
  const item = (id: number, hue: number | null) => ({ id, hue })

  test('没有颜色的挪到有颜色的后面，两边各自保持顺序', () => {
    expect(colouredFirst([item(1, null), item(2, 10), item(3, null), item(4, 200)]).map((i) => i.id)).toEqual([2, 4, 1, 3])
  })

  test('色相 0 是一个颜色，不是没有', () => {
    expect(colouredFirst([item(1, null), item(2, 0)]).map((i) => i.id)).toEqual([2, 1])
  })

  test('返回新数组，不改输入', () => {
    const input = [item(1, null), item(2, 10)]
    const out = colouredFirst(input)
    expect(out).not.toBe(input)
    expect(input.map((i) => i.id)).toEqual([1, 2])
  })
})
