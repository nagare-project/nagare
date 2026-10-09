import { apiFetch } from './api'
import type { LibraryCluster } from './endpoints'

/**
 * 本地作品分组 ↔ 目录作品的手动关联（契约见 docs/library-association.md）。
 * 自动匹配会认错，认错之后弹幕和「看完」都记到别的作品上；关联让用户说了算。
 */

/** 归组置信度低于该值、又没认过作品时标「待确认」 */
export const LOW_CONFIDENCE_THRESHOLD = 0.7

/** 已经看完并回写到了【别的】作品的几集。nagare 不撤销，界面给出去那部作品改进度的入口 */
export interface SyncedRecord {
  anilistId: number
  title?: string
  episodes: number[]
}

export interface LibraryAssociation {
  /** manual：认定为某部目录作品；none：不是目录里的作品（不匹配、不回写） */
  mode: 'manual' | 'none'
  anilistId?: number
  title?: string
  setAt: number
  syncedElsewhere?: SyncedRecord[]
}

/** 没认过的分组里自动匹配到的作品（簇内匹配最多的那一部） */
export interface LibraryMatch {
  anilistId: number
  title?: string
}

export type AssociationChange = { mode: 'manual'; anilistId: number } | { mode: 'none' } | { mode: 'auto' }

interface AssociationData {
  association: LibraryAssociation | null
}

function sendJson(path: string, method: 'PUT' | 'POST', body: unknown): Promise<AssociationData> {
  return apiFetch<AssociationData>(path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

/** 设定 / 清除关联；回到自动匹配时返回 null */
export async function setAssociation(clusterKey: string, change: AssociationChange): Promise<LibraryAssociation | null> {
  return (await sendJson('/api/library/association', 'PUT', { clusterKey, ...change })).association
}

/** 去掉「已回写到别的作品」里某部作品的记录（用户去那部作品改完进度之后） */
export async function dismissSyncedElsewhere(clusterKey: string, anilistId: number): Promise<LibraryAssociation | null> {
  return (await sendJson('/api/library/association/dismiss-synced', 'POST', { clusterKey, anilistId })).association
}

/**
 * 作品分组显示用的标题：认定过作品用作品名；没认定过但自动认出了作品，也用作品名
 * （文件名里的标题常常是罗马音、繁体或干脆是字幕组名）；都没有时才用从文件名解析出的标题。
 */
export function clusterDisplayTitle(cluster: LibraryCluster): string {
  const { association, matched } = cluster
  if (association?.mode === 'manual' && association.title) return association.title
  if (association === undefined && matched?.title) return matched.title
  return cluster.title
}

/** 归组可能不准、用户还没确认过对应作品 */
export function needsConfirmation(cluster: LibraryCluster): boolean {
  return cluster.association === undefined && cluster.confidence < LOW_CONFIDENCE_THRESHOLD
}

/** 集号列表的中文写法：连续的并成区间，如 [1,2,3,5] → 「第 1–3、5 集」 */
export function formatEpisodeList(episodes: number[]): string {
  if (episodes.length === 0) return ''
  const sorted = [...new Set(episodes)].sort((a, b) => a - b)
  const parts: string[] = []
  for (let i = 0; i < sorted.length; ) {
    let j = i
    while (j + 1 < sorted.length && sorted[j + 1] === sorted[j]! + 1) j++
    parts.push(j > i ? `${sorted[i]}–${sorted[j]}` : String(sorted[i]))
    i = j + 1
  }
  return `第 ${parts.join('、')} 集`
}
