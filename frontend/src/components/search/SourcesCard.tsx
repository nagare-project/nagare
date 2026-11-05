import type { UseSourcesResult } from '../../hooks/useSources'
import { mono } from '../../theme'
import { RulesConfigForm } from './RulesConfigForm'
import { SourceList } from './SourceList'
import '../../styles/panels.css'
import './sources.css'

export interface SourcesCardProps {
  sources: UseSourcesResult
  /** 标题级别：设置页里是分区（h2）下的面板（h3）；扩展页里直接挂在页面标题下（h2） */
  headingLevel?: 2 | 3
}

/**
 * 「磁力源规则」面板：规则来源配置 + 规则加载错误 + 已加载规则列表。
 * 设置页「来源」与扩展页共用；数据与动作全部来自 useSources，本组件只做装配。
 */
export function SourcesCard({ sources, headingLevel = 3 }: SourcesCardProps) {
  const { state } = sources
  const Title = headingLevel === 2 ? 'h2' : 'h3'
  const Subtitle = headingLevel === 2 ? 'h3' : 'h4'

  return (
    <section className="info-panel" aria-labelledby="sources-heading">
      <Title id="sources-heading" className="info-panel-title">
        磁力源规则
      </Title>
      <p className="info-panel-text">nagare 不内置任何磁力源。规则由你自己提供：一个规则仓库的 HTTPS 地址，或本机目录；搜索在你自己的电脑上执行。</p>

      {state.phase === 'loading' && (
        <p className="result result--dim" role="status">
          正在读取规则 …
        </p>
      )}
      {state.phase === 'error' && (
        <div className="form-actions">
          <p className="result result--err" role="alert">
            {state.message}
          </p>
          <button type="button" className="btn btn--sm" onClick={() => void sources.reload()}>
            重试
          </button>
        </div>
      )}
      {state.phase === 'ready' && (
        <>
          <RulesConfigForm rules={state.data.rules} onSave={sources.saveConfig} onSync={sources.sync} onReload={sources.reloadRules} />
          <RulesErrors errors={state.data.rules.errors} />
          <hr className="info-panel-rule" />
          <Subtitle className="info-panel-subtitle source-list-heading">
            已加载规则 · <span style={mono}>{state.data.sources.length}</span>
          </Subtitle>
          <SourceList sources={state.data.sources} onToggle={sources.setEnabled} onSelfCheck={sources.selfCheck} />
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
      <p className="rules-errors-title">{errors.length} 条规则加载失败</p>
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
