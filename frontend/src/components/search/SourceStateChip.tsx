import { inFlight } from '../../lib/busy'
import type { SourceOutcome } from '../../lib/endpoints'
import { mono } from '../../theme'
import { describeOutcome, isSourceEnabled } from './sourceState'
import './search.css'

export interface SourceStateChipProps {
  /** 展示名（规则 name）；查不到名字时传 id */
  name: string
  outcome: SourceOutcome
  /**
   * 传入即成为可点击的启用 / 禁用开关（搜索页源状态条）；
   * 不传则是只读状态徽标（设置页自检结果）。
   */
  onToggle?: (enabled: boolean) => void
  /** 切换请求在途：禁用点击 */
  busy?: boolean
}

/**
 * 源状态 chip：`名称 · N 条` / `无结果` / `源异常` / `连接失败` / `已禁用`。
 * 颜色随 state 分级（ok 青 · zero 灰 · dead 红 · failed 橙 · disabled 暗），
 * reason / detail 走 title（hover）与 data-tooltip（键盘聚焦时由 CSS 画出）。
 */
export function SourceStateChip({ name, outcome, onToggle, busy = false }: SourceStateChipProps) {
  const view = describeOutcome(outcome)
  const enabled = isSourceEnabled(outcome)
  const className = `src-chip src-chip--${outcome.state}`

  const body = (
    <>
      <span className="src-chip-dot" aria-hidden="true" />
      <span className="src-chip-name">{name}</span>
      <span className="src-chip-sep" aria-hidden="true">
        ·
      </span>
      <span className="src-chip-state" style={mono}>
        {view.label}
      </span>
    </>
  )

  if (onToggle === undefined) {
    return (
      <span
        className={className}
        role="status"
        title={view.tooltip}
        data-tooltip={view.tooltip}
        aria-label={`${name}：${view.description}`}
        data-state={outcome.state}
      >
        {body}
      </span>
    )
  }

  // 名字取自按钮上的字，再补一段只给读屏的说明：不用 aria-label 盖掉看得见的字，
  // 否则「源甲 · 3 条」与名字对不上，语音控制按看到的字叫它叫不动（WCAG 2.5.3）
  return (
    <button
      type="button"
      className={className}
      title={view.tooltip}
      data-tooltip={view.tooltip}
      aria-pressed={enabled}
      data-state={outcome.state}
      {...inFlight(busy, () => onToggle(!enabled))}
    >
      {body}
      {/* 名字末尾说清这个开关管什么（「启用此源」），开没开由 aria-pressed 念（「已按下」= 启用）；
          不再说「点击禁用 / 启用」—— 那句随状态翻转，和按下状态说的是同一件事 */}
      <span className="visually-hidden">{outcome.state === 'ok' ? '，正常' : ''}，启用此源</span>
    </button>
  )
}
