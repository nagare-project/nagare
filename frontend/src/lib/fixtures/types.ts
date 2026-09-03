/**
 * 假数据的共用形状。
 *
 * ⚠️ 这些类型【只】服务于界面演示。真接口接上后，各页改用 lib/endpoints.ts 里
 * 的真实类型，本目录整个删掉。缺口清单见仓库根的 todos.md。
 *
 * 作品卡的形状【不在这里】：那是 components/media/types.ts 的 MediaSummary，
 * 由组件自己定义。方向是「fixture 去满足组件的契约」，不是反过来 ——
 * 反过来的话删 fixture 就等于给共享组件重新定型（有测试守着，见 fixtures.test.ts）。
 */

/** 放送表里的一条：某天某时播出某作品的某一集 */
export interface FakeAiring {
  id: number
  title: string
  episode: number
  /** ISO 8601，本地时区 */
  airingAt: string
}
