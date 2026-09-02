import type { SourceInfo, SourceOutcome } from '../../lib/endpoints'
import { mono } from '../../tokens'
import { SourceStateChip } from './SourceStateChip'
import './sources.css'

/** 单个源的自检状态 */
export type SelfCheckState =
  | { phase: 'busy' }
  | { phase: 'done'; outcome: SourceOutcome }
  | { phase: 'error'; message: string }

export interface SourceRowProps {
  source: SourceInfo
  check: SelfCheckState | undefined
  /** 有任一切换在途时开关一起禁用 */
  toggling: boolean
  onToggle: (enabled: boolean) => void
  onSelfCheck: () => void
}

/** 只把 http(s) 链接渲染成 <a>；规则文件是用户提供的，javascript: 之类不该变成可点的东西 */
const SAFE_HTTP_URL = /^https?:\/\//i

/**
 * 已加载规则列表的一行：名称 · id · 主页 · 能力标记 · 启用开关 · 自检 · 自检结果。
 */
export function SourceRow({ source, check, toggling, onToggle, onSelfCheck }: SourceRowProps) {
  const { id, name, homepage, enabled, capabilities, hasSelfTest } = source
  const checking = check?.phase === 'busy'

  return (
    <li className={enabled ? 'source-row' : 'source-row source-row--disabled'} data-source-id={id}>
      <div className="source-main">
        <span className="source-name">{name}</span>
        <span className="source-id" style={mono}>
          {id}
        </span>
        <HomepageLink homepage={homepage} name={name} />
      </div>

      <div className="source-meta">
        {capabilities.seeders && (
          <span className="badge badge--accent" title="该源提供做种数">
            做种数
          </span>
        )}
        <span className="source-priority" style={mono} title="优先级（数值越小越靠前）">
          P{capabilities.priority}
        </span>
      </div>

      <div className="source-actions">
        <button
          type="button"
          role="switch"
          className="switch"
          aria-checked={enabled}
          aria-label={`启用 ${name}`}
          onClick={() => onToggle(!enabled)}
          disabled={toggling}
        >
          <span className="switch-knob" aria-hidden="true" />
        </button>
        <button
          type="button"
          className="hud-button hud-button--small hud-button--ghost"
          onClick={onSelfCheck}
          disabled={!hasSelfTest || checking}
          title={hasSelfTest ? '用规则自带的关键词探活' : '该规则未提供自检关键词'}
          aria-label={`自检 ${name}`}
        >
          {checking ? '自检中 …' : '自检'}
        </button>
      </div>

      <SelfCheckResult name={name} check={check} />
    </li>
  )
}

function HomepageLink({ homepage, name }: { homepage: string; name: string }) {
  if (homepage === '') return null
  if (!SAFE_HTTP_URL.test(homepage)) {
    return (
      <span className="source-home-text" style={mono} title="主页不是 http(s) 链接，不予打开">
        {homepage}
      </span>
    )
  }
  return (
    <a
      className="hud-link source-home"
      href={homepage}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={`${name} 主页（新标签页打开）`}
    >
      主页 ↗
    </a>
  )
}

/** 自检结果行：busy / 徽标（与搜索页同一套五态）/ 请求失败 */
function SelfCheckResult({ name, check }: { name: string; check: SelfCheckState | undefined }) {
  if (check === undefined) return null
  if (check.phase === 'busy') {
    return (
      <p className="source-check result result--dim" style={mono} role="status">
        正在自检 …
      </p>
    )
  }
  if (check.phase === 'error') {
    return (
      <p className="source-check result result--err" style={mono} role="alert">
        自检请求失败：{check.message}
      </p>
    )
  }
  return (
    <div className="source-check">
      <SourceStateChip name={name} outcome={check.outcome} />
      {check.outcome.reason !== undefined && check.outcome.reason !== '' && (
        <span className="source-check-reason">{check.outcome.reason}</span>
      )}
    </div>
  )
}
