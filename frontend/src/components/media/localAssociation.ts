import type { LibraryCluster, LibraryData } from '../../lib/endpoints'
import { seasonConflict } from '../../lib/librarySeries'

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
  /**
   * 自动选中的本地作品分组。同一部作品的多个版本（不同字幕组、不同文件夹）一起选中，
   * 剧集按集号合并显示（见 lib/librarySeries.ts 的 mergeSeries）；空数组表示没有可代选的。
   */
  selected: LibraryCluster[]
  /** 依据：manual 全部是后端认定的；legacy 本机旧记录（要迁到后端）；matched 含自动匹配的（用户没确认过） */
  via?: 'manual' | 'legacy' | 'matched'
  /** 认定为这部作品的全部本地分组 */
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
  // 自动认出是这部作品、用户还没认定过的分组（扫描后的后台识别会给每个分组认一部）。
  // 季数和认定过的（或第一个认出的）版本对不上的不代选：多半是认错了季，并进来就成了「同一集的另一个版本」
  const autoMatched = data.clusters.filter((c) => c.association === undefined && c.matched?.anilistId === anilistId)
  const base = associated[0] ?? autoMatched[0]
  const matched = autoMatched.filter((c) => base === undefined || !seasonConflict(base, c))
  const result: LocalResolution = {
    selected: [],
    associated,
    legacyMissing: legacy !== null && legacyCluster === undefined && associated.length === 0,
    // 作品已经有了后端认定时，旧记录不再采用：否则那个认定哪天被撤掉，一条早就作废的选择会悄悄复活
    dropLegacy: legacy !== null && (legacyCluster?.association !== undefined || associated.length > 0),
  }
  // 认定过的与自动认出的一起选中、合并显示：同一部番的几个版本本来就该在一张剧集表里。
  // 自动认出的不算用户确认过（不自动播放；播放哪一集就认定那一集所在的分组）
  if (associated.length > 0) return { ...result, selected: [...associated, ...matched], via: matched.length > 0 ? 'matched' : 'manual' }
  if (legacyCluster && legacyCluster.association === undefined) return { ...result, selected: [legacyCluster], via: 'legacy' }
  if (matched.length > 0) return { ...result, selected: matched, via: 'matched' }
  return result
}
