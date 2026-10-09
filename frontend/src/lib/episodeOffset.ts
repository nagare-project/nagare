import { apiFetch, ApiError } from './api'

/**
 * 一部作品之前（沿前作关系）一共有多少集 TV 正片。字幕组常常跨季连续编号：
 * 前作 12 集时，第二季第 3 集在发布标题里叫 15。
 *
 * known 为 false 表示 animego 算不出来 —— 与「前面没有作品」（known=true、offset=0）是两回事，
 * 绝不能当 0 用：拿没人确认过的起点去换算集号，正是这个接口要防的错。
 */
export interface EpisodeOffset {
  known: boolean
  offset: number
}

/** GET /api/anime/{id}/episode-offset（公开元数据，只读 animego 的元数据线） */
export async function fetchEpisodeOffset(anilistId: number, signal?: AbortSignal): Promise<EpisodeOffset> {
  const data = await apiFetch<unknown>(`/api/anime/${anilistId}/episode-offset`, signal === undefined ? undefined : { signal })
  if (typeof data !== 'object' || data === null || typeof (data as { known?: unknown }).known !== 'boolean') {
    throw new ApiError('集号偏移的响应格式无效', 200)
  }
  const { known, offset } = data as { known: boolean; offset?: unknown }
  if (!known || typeof offset !== 'number' || !Number.isSafeInteger(offset) || offset < 0) return { known: false, offset: 0 }
  return { known: true, offset }
}

/** 偏移能不能拿来换算：已知且确有前作（offset 为 0 时两种编号本来就一样） */
export function usableOffset(offset: EpisodeOffset | null | undefined): number | undefined {
  return offset?.known === true && offset.offset > 0 ? offset.offset : undefined
}
