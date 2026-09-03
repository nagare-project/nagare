import type { LibraryDropGroup, LibraryFolder } from '../../lib/endpoints'
import { mono } from '../../theme'

/**
 * 「这次扫描跳过了什么」。
 *
 * 存在的理由：扫描会丢文件，而且是【静默地】丢 —— 用户唯一的察觉方式是
 * 「我那一集怎么不在库里」，界面什么都不说。这个组件就是把那句话补上。
 *
 * 两条设计约束：
 *  1. 什么都没丢时【完全不渲染】。每次扫描都吓用户一跳是另一种病。
 *  2. 每一条都要给恢复动作。只说「跳过了 3 个」而不说怎么找回来，
 *     等于把静默失败换成了嘈杂失败。
 *
 * 文案与恢复动作全部由后端给（`internal/library/drops.go` 的 dropCopy）：
 * 判断在哪里做，话就在哪里说，免得两边各写一套慢慢对不上。
 */

/** 合并显示的样本条数上限；count 仍是真实总数 */
const SAMPLE_LIMIT = 5

/**
 * 把各库目录的丢弃按 reason 合并。
 * 组的先后沿用后端顺序（后端已按「用户改得动」排过），不在这里重排。
 */
export function mergeDrops(folders: LibraryFolder[]): LibraryDropGroup[] {
  const byReason = new Map<string, LibraryDropGroup>()
  for (const folder of folders) {
    for (const group of folder.dropped?.groups ?? []) {
      const seen = byReason.get(group.reason)
      if (seen === undefined) {
        byReason.set(group.reason, { ...group, samples: group.samples.slice(0, SAMPLE_LIMIT) })
        continue
      }
      seen.count += group.count
      seen.samples = [...seen.samples, ...group.samples].slice(0, SAMPLE_LIMIT)
    }
  }
  return [...byReason.values()]
}

interface ScanDropsProps {
  folders: LibraryFolder[]
  /** 默认展开。库里一部作品都没有时用 —— 那时原因就是全部内容，不该藏起来 */
  open?: boolean
}

export function ScanDrops({ folders, open = false }: ScanDropsProps) {
  const groups = mergeDrops(folders)
  if (groups.length === 0) return null
  const total = groups.reduce((n, g) => n + g.count, 0)

  return (
    <details className="drops" open={open}>
      {/* 「项」而不是「文件」：too-deep / unreadable-dir 计的是目录 */}
      <summary className="drops-summary">
        <span className="drops-count">{total} 项没有进库</span>
        <span className="drops-why">{groups.map((g) => g.message).join('；')}</span>
      </summary>
      <ul className="drops-list">
        {groups.map((g) => (
          <DropGroupRow key={g.reason} group={g} />
        ))}
      </ul>
    </details>
  )
}

function DropGroupRow({ group }: { group: LibraryDropGroup }) {
  const hidden = group.count - group.samples.length
  return (
    <li className="drops-group">
      <p className="drops-group-head">
        <strong>{group.count} 项</strong>：{group.message}
      </p>
      <p className="drops-recovery">→ {group.recovery}</p>
      <ul className="drops-paths">
        {group.samples.map((path) => (
          <li key={path} className="drops-path" style={mono}>
            {path}
          </li>
        ))}
        {hidden > 0 && <li className="drops-path drops-path--more">…另有 {hidden} 项</li>}
      </ul>
    </li>
  )
}
