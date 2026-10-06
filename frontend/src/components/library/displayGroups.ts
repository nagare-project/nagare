import type { LibraryCluster, LibraryGroup, LibraryItem } from '../../lib/endpoints'

/** 后端根目录分组的哨兵键：根下只有一个文件时是 `__root__`，多个时逐个成组为 `__root__/<文件名>` */
const ROOT_GROUP_KEY = '__root__'

function isRootGroup(group: LibraryGroup): boolean {
  return group.groupKey === ROOT_GROUP_KEY || group.groupKey.startsWith(`${ROOT_GROUP_KEY}/`)
}

/** 集号升序；没有集号的排在最后，按文件名自然序 */
function byEpisodeThenName(a: LibraryItem, b: LibraryItem): number {
  const left = a.episode ?? Infinity
  const right = b.episode ?? Infinity
  if (left !== right) return left - right
  return a.fileName.localeCompare(b.fileName, 'zh-CN', { numeric: true })
}

/**
 * 单部作品页上的剧集分区。
 *
 * 后端（照搬 animego 的分组算法）把媒体库根目录下的散文件逐个独立成组，
 * 为的是归簇时不把不同的番错合进同一个目录组。到了作品页，这些文件已经确定
 * 属于同一部，再「一个文件一个分区、分区标题是完整文件名」地摊开就只剩噪音。
 * 这里把它们并回一个分区（标题留空，由调用方显示为「剧集」），放在第一个根分组
 * 原来的位置；子目录分组原样保留。
 */
export function displayGroups(cluster: LibraryCluster): LibraryGroup[] {
  const firstRoot = cluster.groups.findIndex(isRootGroup)
  if (firstRoot === -1) return cluster.groups
  const merged: LibraryGroup = {
    groupKey: ROOT_GROUP_KEY,
    label: '',
    sortMode: 'episode',
    items: cluster.groups.filter(isRootGroup).flatMap((group) => group.items).sort(byEpisodeThenName),
  }
  const others = cluster.groups.filter((group) => !isRootGroup(group))
  return [...others.slice(0, firstRoot), merged, ...others.slice(firstRoot)]
}
