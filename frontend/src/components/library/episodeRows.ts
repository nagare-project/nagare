import type { LibraryCluster, LibraryItem } from '../../lib/endpoints'
import { isPlayable } from '../../lib/librarySeries'
import { episodeHasAired } from '../media/episode-metadata'
import type { EpisodeMetadata, MediaSummary } from '../media/types'
import { displayGroups } from './displayGroups'

/**
 * 作品页集数列表的数据：本地文件按分区排好，目录里已播出、库里却没有的集插进去置灰。
 * 只做数据，不碰界面（EpisodeList 负责画），所以能单独表测。
 */

/** 列表里的一行：本地有文件的集，或目录里有、库里没有的集 */
export type EpisodeRowData =
  | { kind: 'local'; item: LibraryItem; meta?: EpisodeMetadata }
  | { kind: 'missing'; meta: EpisodeMetadata }

export interface EpisodeSection {
  key: string
  /** 分区标题：子文件夹名；只有一个分区或分区就是作品本身时是「剧集」 */
  label: string
  rows: EpisodeRowData[]
}

export interface EpisodeListData {
  sections: EpisodeSection[]
  /** 附加内容（特典、片头片尾、菜单…）：不算集，收在列表末尾 */
  extras: LibraryItem[]
  /** 本地有可当成一集播放的文件 */
  hasPlayable: boolean
}

const MAIN_LABEL = '剧集'
const MISSING_LABEL = '库里没有的集'

const byEpisode = (a: EpisodeRowData, b: EpisodeRowData): number => rowEpisode(a) - rowEpisode(b)

function rowEpisode(row: EpisodeRowData): number {
  if (row.kind === 'missing') return row.meta.episode
  return row.item.episode ?? Infinity
}

/**
 * catalog 缺省 = 没有目录资料（本地分组页）：只列本地文件。
 * cluster 缺省 = 本地没有这部作品的文件：目录里已播出的集全部置灰列出。
 */
export function buildEpisodeList(cluster: LibraryCluster | undefined, catalog?: MediaSummary, now = Date.now()): EpisodeListData {
  const metaByEpisode = new Map((catalog?.episodeTitles ?? []).map((meta) => [meta.episode, meta]))
  const metaOf = (item: LibraryItem) => (item.episode === null ? undefined : metaByEpisode.get(item.episode))
  const extras: LibraryItem[] = []
  const sections: EpisodeSection[] = []
  const groups = cluster === undefined ? [] : displayGroups(cluster)
  for (const group of groups) {
    const rows: EpisodeRowData[] = []
    for (const item of group.items) {
      if (isPlayable(item)) rows.push({ kind: 'local', item, ...withMeta(metaOf(item)) })
      else extras.push(item)
    }
    if (rows.length === 0) continue
    const label = group.label && group.label !== cluster?.title ? group.label : MAIN_LABEL
    sections.push({ key: group.groupKey, label, rows })
  }

  const local = new Set(sections.flatMap((section) => section.rows).map((row) => (row.kind === 'local' ? row.item.episode : null)))
  const missing: EpisodeRowData[] = catalog === undefined ? [] : [...metaByEpisode.values()]
    .filter((meta) => !local.has(meta.episode) && episodeHasAired(meta, catalog, now))
    .map((meta) => ({ kind: 'missing', meta }))
  const hasPlayable = sections.length > 0
  // 库里没有的集插回唯一的那个分区（按集号排好）；本地分成了几个子文件夹时说不准该插哪，单独成一节
  if (missing.length > 0 && sections.length === 1) {
    const only = sections[0]!
    sections[0] = { ...only, rows: [...only.rows, ...missing].sort(byEpisode) }
  } else if (missing.length > 0) {
    sections.push({ key: '__missing__', label: hasPlayable ? MISSING_LABEL : MAIN_LABEL, rows: missing.sort(byEpisode) })
  }
  return { sections, extras, hasPlayable }
}

function withMeta(meta: EpisodeMetadata | undefined): { meta?: EpisodeMetadata } {
  return meta === undefined ? {} : { meta }
}
