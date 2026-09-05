/**
 * 元数据卡片层【自己的】数据契约。
 *
 * 为什么单独一份、而不是直接用 lib/fixtures 里的类型 —— 那正是本仓踩过的坑：
 * MediaCard 与 DiscoverHero 是两个已发布的共享组件，它们原本以 `FakeMedia`
 * 为 prop 类型。那意味着将来接上真接口时，「删掉 fixture 目录」不是删一个目录，
 * 是给两个共享组件重新定型 —— 假数据的形状已经从演示层爬进了产品层。
 *
 * 现在方向反过来：组件定契约，fixture 去满足它。真接口来的时候，
 * 改的是「谁来生产 MediaSummary」，组件一行不动。
 *
 * ⚠️ 这里的字段是【卡片渲染需要什么】，不是「后端将来会返回什么」。
 * 它是猜出来的形状，但猜的是我们自己的界面需求，不是别人的接口 ——
 * 前者我们说了算，后者说了不算。
 */

/** 一部作品在网格 / 轮播里的最小投影 */
export interface MediaSummary {
  id: number
  title: string
  /** 原文标题，副标题位展示 */
  titleNative?: string
  titleEnglish?: string
  /** 同源图片地址，沿用本机封面缓存或本地静态资源。 */
  cover?: string
  banner?: string
  /** YouTube 预告片 ID，仅用于 Discover 的静音背景。 */
  trailerId?: string
  year?: number
  season?: string
  /** 总集数；未知为 null（剧场版 / 连载中） */
  episodes: number | null
  /** 已看到第几集；未开始为 0 */
  watched: number
  score?: number
  genres: string[]
  /** 简介。hero 轮播与卡片浮层用 */
  description?: string
  status?: string
  format?: string
  duration?: number
  source?: string
  studios?: string[]
  startDate?: string
  nextAiring?: { episode: number; at: number }
  recentAiring?: { episode: number; at: number }
  relations?: { type: string; media: MediaSummary }[]
  recommendations?: MediaSummary[]
  characters?: { name: string; image: string; role: string; actor?: string; actorImage?: string }[]
  rankings?: { rank: number; type: string; context: string; year: number; season: string }[]
}
