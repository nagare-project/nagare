import { useState } from 'react'
import type { SourceInfo, SourceOutcome } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { mono } from '../../tokens'
import { SourceRow } from './SourceRow'
import type { SelfCheckState } from './SourceRow'
import './sources.css'

export interface SourceListProps {
  sources: SourceInfo[]
  onToggle: (id: string, enabled: boolean) => Promise<void>
  onSelfCheck: (id: string) => Promise<SourceOutcome>
}

/**
 * 已加载规则列表：每行一个开关 + 自检。切换失败在列表底部提示；
 * 自检结果按源各自保存，互不干扰。
 */
export function SourceList({ sources, onToggle, onSelfCheck }: SourceListProps) {
  const [togglingId, setTogglingId] = useState<string | null>(null)
  const [toggleError, setToggleError] = useState<string | null>(null)
  const [checks, setChecks] = useState<Record<string, SelfCheckState>>({})

  async function handleToggle(id: string, enabled: boolean): Promise<void> {
    if (togglingId !== null) return
    setTogglingId(id)
    setToggleError(null)
    try {
      await onToggle(id, enabled)
    } catch (err) {
      console.error('切换源启用状态失败', err)
      setToggleError(errorText(err, '切换源启用状态失败'))
    } finally {
      setTogglingId(null)
    }
  }

  async function handleSelfCheck(id: string): Promise<void> {
    setChecks((prev) => ({ ...prev, [id]: { phase: 'busy' } }))
    try {
      const outcome = await onSelfCheck(id)
      setChecks((prev) => ({ ...prev, [id]: { phase: 'done', outcome } }))
    } catch (err) {
      console.error('自检失败', err)
      setChecks((prev) => ({ ...prev, [id]: { phase: 'error', message: errorText(err, '自检失败') } }))
    }
  }

  if (sources.length === 0) {
    return (
      <p className="result result--dim" style={mono}>
        还没有加载任何规则。填好规则来源后点「同步规则」或「重新加载」。
      </p>
    )
  }

  return (
    <>
      <ul className="source-list">
        {sources.map((source) => (
          <SourceRow
            key={source.id}
            source={source}
            check={checks[source.id]}
            toggling={togglingId !== null}
            onToggle={(enabled) => void handleToggle(source.id, enabled)}
            onSelfCheck={() => void handleSelfCheck(source.id)}
          />
        ))}
      </ul>
      {toggleError !== null && (
        <p className="result result--err" style={mono} role="alert">
          {toggleError}
        </p>
      )}
    </>
  )
}
