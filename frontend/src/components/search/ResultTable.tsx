import type { SearchItem } from '../../lib/endpoints'
import { mono } from '../../tokens'
import { ResultRow } from './ResultRow'
import './search.css'

/** 最多渲染多少行；再多就提示换更具体的关键词（检索工具不需要翻页） */
export const MAX_RENDERED_RESULTS = 200

export interface ResultTableProps {
  items: SearchItem[]
  /** 规则 id → 展示名 */
  names: Record<string, string>
}

/** 结果表：紧凑密度、mono 数字；「做种」列只在有任一条带 seeders 时出现 */
export function ResultTable({ items, names }: ResultTableProps) {
  const showSeeders = items.some((item) => typeof item.seeders === 'number')
  const isTruncated = items.length > MAX_RENDERED_RESULTS
  const visible = isTruncated ? items.slice(0, MAX_RENDERED_RESULTS) : items

  return (
    <div className="res-wrap">
      <table className="res-table">
        <caption className="visually-hidden">搜索结果，共 {items.length} 条</caption>
        <thead>
          <tr>
            <th scope="col" className="res-title">
              标题
            </th>
            <th scope="col" className="res-size">
              体积
            </th>
            <th scope="col" className="res-fansub">
              字幕组
            </th>
            <th scope="col" className="res-date">
              日期
            </th>
            {showSeeders && (
              <th scope="col" className="res-seeders">
                做种
              </th>
            )}
            <th scope="col" className="res-source">
              来源
            </th>
            <th scope="col" className="res-actions">
              操作
            </th>
          </tr>
        </thead>
        <tbody>
          {visible.map((item, index) => (
            <ResultRow
              // 同一 infohash 可能来自多个源；行不重排，source + 序号足够稳定
              key={`${item.source}#${index}`}
              item={item}
              sourceName={names[item.source] ?? item.source}
              showSeeders={showSeeders}
            />
          ))}
        </tbody>
      </table>
      {isTruncated && (
        <p className="result result--dim res-truncated" style={mono} role="status">
          只显示前 {MAX_RENDERED_RESULTS} 条（共 {items.length} 条），换个更具体的关键词缩小范围
        </p>
      )}
    </div>
  )
}
