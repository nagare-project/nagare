import type { SourceOutcome } from '../../lib/endpoints'
import { SourceStateChip } from './SourceStateChip'
import './search.css'

export interface SourceStatusBarProps {
  outcomes: SourceOutcome[]
  /** 规则 id → 展示名；缺失时回退显示 id */
  names: Record<string, string>
  /** 点 chip 切换该源启用状态（页面层负责调接口并重搜） */
  onToggle: (id: string, enabled: boolean) => void
  /** 有切换在途时所有 chip 一起禁用，避免两个请求交错 */
  busy: boolean
}

/**
 * 源状态条：每个源一个 chip，一眼分清「都没结果」还是「源坏了」。
 * 空结果页上它是主角，所以永远渲染在结果表之前。
 */
export function SourceStatusBar({ outcomes, names, onToggle, busy }: SourceStatusBarProps) {
  if (outcomes.length === 0) return null
  return (
    <ul className="src-bar" aria-label="各源状态">
      {outcomes.map((outcome) => (
        <li key={outcome.source}>
          <SourceStateChip
            name={names[outcome.source] ?? outcome.source}
            outcome={outcome}
            onToggle={(enabled) => onToggle(outcome.source, enabled)}
            busy={busy}
          />
        </li>
      ))}
    </ul>
  )
}
