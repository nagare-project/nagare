import type { LibraryCluster, LibraryData } from '../../lib/endpoints'

/**
 * 目录作品页（/entry）上的「本地媒体库」该显示哪个本地作品分组。
 *
 * 关联以后端为准（docs/library-association.md）。以前作品页的选择只记在浏览器的
 * localStorage 里（`nagare:media-library:<作品 ID>` → clusterKey），换浏览器就丢；
 * 这里把还没迁走的旧记录认出来，交给调用方同步到后端后删掉。
 */

const legacyKey = (anilistId: number) => `nagare:media-library:${anilistId}`

/** 读本机旧记录；存储被禁用时当没有 */
export function readLegacyChoice(anilistId: number): string | null {
  try {
    return localStorage.getItem(legacyKey(anilistId))
  } catch {
    return null
  }
}

/** 删掉本机旧记录（已经同步到后端，或后端已经有了别的决定） */
export function forgetLegacyChoice(anilistId: number): void {
  try {
    localStorage.removeItem(legacyKey(anilistId))
  } catch {
    // 删不掉也无妨：后端已有关联时旧记录不会再被采用
  }
}

export interface LocalResolution {
  /** 自动选中的本地作品分组 */
  selected?: LibraryCluster
  /** 依据：manual 后端认定；legacy 本机旧记录（要迁到后端）；matched 自动匹配（用户没确认过） */
  via?: 'manual' | 'legacy' | 'matched'
  /** 认定为这部作品的全部本地分组（不止一个时让用户挑） */
  associated: LibraryCluster[]
  /** 本机旧记录指向的分组已经不在媒体库里（盘没插、文件删了） */
  legacyMissing: boolean
  /** 旧记录已经没用了：那个分组在后端已经有了决定，或这部作品已经认定了别的分组 */
  dropLegacy: boolean
}

/** 认定为这部作品的分组 */
export function isAssociatedWith(cluster: LibraryCluster, anilistId: number): boolean {
  return cluster.association?.mode === 'manual' && cluster.association.anilistId === anilistId
}

export function resolveLocalCluster(data: LibraryData, anilistId: number, legacy: string | null): LocalResolution {
  const associated = data.clusters.filter((c) => isAssociatedWith(c, anilistId))
  const legacyCluster = legacy === null ? undefined : data.clusters.find((c) => c.clusterKey === legacy)
  const result: LocalResolution = {
    associated,
    legacyMissing: legacy !== null && legacyCluster === undefined && associated.length === 0,
    // 作品已经有了后端认定时，旧记录不再采用：否则那个认定哪天被撤掉，一条早就作废的选择会悄悄复活
    dropLegacy: legacy !== null && (legacyCluster?.association !== undefined || associated.length > 0),
  }
  if (associated.length === 1) return { ...result, selected: associated[0], via: 'manual' }
  if (associated.length > 1) return result
  if (legacyCluster && legacyCluster.association === undefined) return { ...result, selected: legacyCluster, via: 'legacy' }
  // 自动匹配只在恰好一个分组时代选，并且不算用户确认过（不自动播放、不自动认定）
  const matched = data.clusters.filter((c) => c.association === undefined && c.matched?.anilistId === anilistId)
  if (matched.length === 1) return { ...result, selected: matched[0], via: 'matched' }
  return result
}
