import type { UseSourcesResult } from '../../hooks/useSources'
import { label, mono } from '../../theme'
import { RulesConfigForm } from './RulesConfigForm'
import { SourceList } from './SourceList'
import './sources.css'

export interface SourcesCardProps {
  sources: UseSourcesResult
}

/**
 * 设置页「磁力源」区块：规则来源配置 + 规则加载错误 + 已加载规则列表。
 * 数据与动作全部来自 useSources，本组件只做装配。
 */
export function SourcesCard({ sources }: SourcesCardProps) {
  const { state } = sources

  return (
    <section className="panel settings-card" aria-labelledby="sources-heading">
      <h2 id="sources-heading" className="panel-heading">
        磁力源
      </h2>
      <p className="page-notice-copy">
        nagare 不内置任何磁力源。规则由你自己提供：一个规则仓库的 HTTPS 地址，或本机目录；
        搜索在你自己的电脑上执行。
      </p>

      {state.phase === 'loading' && (
        <p className="result result--dim" style={mono} role="status">
          正在读取规则 …
        </p>
      )}
      {state.phase === 'error' && (
        <>
          <p className="result result--err" style={mono}>
            {state.message}
          </p>
          <p>
            <button
              type="button"
              className="btn btn--sm"
              onClick={() => void sources.reload()}
            >
              重试
            </button>
          </p>
        </>
      )}
      {state.phase === 'ready' && (
        <>
          <RulesConfigForm
            rules={state.data.rules}
            onSave={sources.saveConfig}
            onSync={sources.sync}
            onReload={sources.reloadRules}
          />
          <RulesErrors errors={state.data.rules.errors} />
          <h3 className="source-list-heading" style={label}>
            已加载规则 · {state.data.sources.length}
          </h3>
          <SourceList
            sources={state.data.sources}
            onToggle={sources.setEnabled}
            onSelfCheck={sources.selfCheck}
          />
        </>
      )}
    </section>
  )
}

/** 规则文件坏了要让人看见：逐条列出 */
function RulesErrors({ errors }: { errors: string[] }) {
  if (errors.length === 0) return null
  return (
    <div className="rules-errors" role="alert">
      <p className="result result--err" style={mono}>
        {errors.length} 条规则加载失败
      </p>
      <ul>
        {errors.map((error) => (
          <li key={error} style={mono}>
            {error}
          </li>
        ))}
      </ul>
    </div>
  )
}
