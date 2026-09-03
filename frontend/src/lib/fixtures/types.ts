/**
 * 假数据的共用形状。
 *
 * ⚠️ 这些类型【只】服务于界面演示。真接口接上后，各页改用 lib/endpoints.ts 里
 * 的真实类型，本目录整个删掉。缺口清单见仓库根的 todos.md。
 */

/** 一部作品在列表/网格里的最小投影 */
export interface FakeMedia {
  id: number
  title: string
  /** 原文标题，副标题位展示 */
  titleNative?: string
  year?: number
  season?: string
  /** 总集数；未知为 null（剧场版/连载中） */
  episodes: number | null
  /** 已看到第几集；未开始为 0 */
  watched: number
  score?: number
  genres: string[]
}

/** 放送表里的一条：某天某时播出某作品的某一集 */
export interface FakeAiring {
  id: number
  title: string
  episode: number
  /** ISO 8601，本地时区 */
  airingAt: string
}
