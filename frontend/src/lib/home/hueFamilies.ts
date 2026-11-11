// 「按色调逛」：本季作品按封面主色分组。
//
// 七个色调按 OKLCH 色相环划分，左闭右开 [lo, hi)，红调跨过 0°。边界刻意不均匀：OKLCH 在蓝紫上
// 占的角度比黄色多，等分的话大多数封面会挤进两组。
//
// 没有色相的作品（上游的占位色、灰色封面）不属于任何一组：把占位紫算进紫调，
// 一堆毫不相干的作品会挤在一个它们都没有的颜色下面。
//
// 纯函数：不碰 DOM、不碰 React。

export type HueFamilyKey = 'red' | 'orange' | 'yellow' | 'green' | 'cyan' | 'blue' | 'purple'

export interface HueFamily {
  key: HueFamilyKey
  /** 下界（含），度 */
  lo: number
  /** 上界（不含），度；跨 0° 时小于 lo */
  hi: number
  /** 代表这一组的角度：色点与选中胶囊的颜色 */
  hue: number
  /** 界面上的名字 */
  label: string
}

export const HUE_FAMILIES: readonly HueFamily[] = [
  { key: 'red', lo: 345, hi: 35, hue: 20, label: '红调' },
  { key: 'orange', lo: 35, hi: 75, hue: 55, label: '橙调' },
  { key: 'yellow', lo: 75, hi: 115, hue: 95, label: '黄调' },
  { key: 'green', lo: 115, hi: 165, hue: 145, label: '绿调' },
  { key: 'cyan', lo: 165, hi: 215, hue: 195, label: '青调' },
  { key: 'blue', lo: 215, hi: 275, hue: 245, label: '蓝调' },
  { key: 'purple', lo: 275, hi: 345, hue: 305, label: '紫调' },
]

function inFamily(family: HueFamily, hue: number): boolean {
  return family.lo > family.hi ? hue >= family.lo || hue < family.hi : hue >= family.lo && hue < family.hi
}

export function hueFamilyOf(hue: number | null): HueFamilyKey | null {
  if (hue === null || !Number.isFinite(hue)) return null
  const normalised = ((hue % 360) + 360) % 360
  return HUE_FAMILIES.find((f) => inFamily(f, normalised))?.key ?? null
}

export interface HueGroup<T> {
  key: HueFamilyKey
  hue: number
  label: string
  items: T[]
}

/** 按色相环的顺序给出各组，组内保持原顺序；空的组不给 —— 打开一个空面板的胶囊是个什么都不做的控件 */
export function groupByHueFamily<T extends { hue: number | null }>(items: readonly T[]): HueGroup<T>[] {
  const buckets = new Map<HueFamilyKey, T[]>()
  for (const item of items) {
    const key = hueFamilyOf(item.hue)
    if (!key) continue
    buckets.set(key, [...(buckets.get(key) ?? []), item])
  }
  return HUE_FAMILIES.filter((f) => buckets.has(f.key)).map((f) => ({
    key: f.key,
    hue: f.hue,
    label: f.label,
    items: buckets.get(f.key) ?? [],
  }))
}

/** 默认打开的一组：最大的那组，一样大时按色相环顺序 */
export function defaultFamily(groups: ReadonlyArray<{ key: HueFamilyKey; items: readonly unknown[] }>): HueFamilyKey | null {
  let best: { key: HueFamilyKey; size: number } | null = null
  for (const g of groups) {
    if (!best || g.items.length > best.size) best = { key: g.key, size: g.items.length }
  }
  return best?.key ?? null
}

/** key 的下一组，最后一组之后回到第一组：一组的横排看到头时把读者送去的地方。没有别的组时为 null */
export function nextFamily(groups: ReadonlyArray<{ key: HueFamilyKey }>, key: HueFamilyKey): HueFamilyKey | null {
  const at = groups.findIndex((g) => g.key === key)
  if (at < 0 || groups.length < 2) return null
  return groups[(at + 1) % groups.length].key
}

/**
 * 有色相的在前、没有的在后，两边各自保持原顺序。
 * 横幅从第一张开始，第一张给整页上色；没有色相的第一张会让发现页一打开是灰的。
 */
export function colouredFirst<T extends { hue: number | null }>(items: readonly T[]): T[] {
  return [...items.filter((x) => x.hue != null), ...items.filter((x) => x.hue == null)]
}
