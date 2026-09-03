import { Link } from '@tanstack/react-router'
import type { SearchResult, SourceOutcome, SourcesData } from '../../lib/endpoints'
import type { SearchState } from '../../hooks/useMagnetSearch'
import type { SourcesState } from '../../hooks/useSources'
import { mono } from '../../tokens'
import type { PlayControl } from './ResultRow'
import { ResultTable } from './ResultTable'
import { SourceStatusBar } from './SourceStatusBar'
import './search.css'

export interface SearchBodyProps {
  query: string
  sources: SourcesState
  search: SearchState
  /** 规则 id → 展示名 */
  names: Record<string, string>
  onRetrySources: () => void
  onRetrySearch: () => void
  onToggleSource: (id: string, enabled: boolean) => void
  /** 切换源在途 */
  toggling: boolean
  /** 磁力播放的占用情况，透传给每一行的播放按钮 */
  play: PlayControl
}

/**
 * 搜索页主体的分派：规则加载态 → 无规则引导 → 未搜索引导 → 搜索错误 → 结果 / 空结果。
 * 三种空态各自一段，空结果那段把源状态条放在最前面（它才是主角）。
 */
export function SearchBody(props: SearchBodyProps) {
  const { query, sources, search } = props

  if (sources.phase === 'loading') {
    return (
      <p className="result result--dim" style={mono} role="status">
        正在读取规则 …
      </p>
    )
  }
  if (sources.phase === 'error') {
    return (
      <RetryNotice title="读取规则失败" message={sources.message} onRetry={props.onRetrySources} />
    )
  }
  // unauthorized 已在页面层拦截
  if (sources.phase !== 'ready') return null

  if (sources.data.sources.length === 0) {
    return <NoSourcesNotice data={sources.data} />
  }
  if (query === '') {
    return <IdleNotice data={sources.data} />
  }
  if (search.phase === 'error') {
    return <RetryNotice title="搜索失败" message={search.message} onRetry={props.onRetrySearch} />
  }

  const result = pickResult(search)
  if (result === null) {
    return (
      <p className="result result--dim" style={mono} role="status">
        正在搜索「{query}」…
      </p>
    )
  }

  const isStale = search.phase === 'searching'
  return (
    <div className={isStale ? 'search-results search-results--stale' : 'search-results'} aria-busy={isStale}>
      <SourceStatusBar
        outcomes={result.sources}
        names={props.names}
        onToggle={props.onToggleSource}
        busy={props.toggling || isStale}
      />
      {result.items.length === 0 ? (
        <EmptyResultsNotice query={result.query} outcomes={result.sources} />
      ) : (
        <ResultTable items={result.items} names={props.names} play={props.play} />
      )}
    </div>
  )
}

/** ready 直接用；searching 时沿用上一次结果（切换源后重搜不闪白） */
function pickResult(search: SearchState): SearchResult | null {
  if (search.phase === 'ready') return search.data
  if (search.phase === 'searching') return search.stale
  return null
}

function RetryNotice({ title, message, onRetry }: { title: string; message: string; onRetry: () => void }) {
  return (
    <div className="page-notice">
      <h2 className="page-notice-title">{title}</h2>
      <p className="page-notice-copy result--err">{message}</p>
      <p className="page-notice-actions">
        <button type="button" className="hud-button hud-button--small" onClick={onRetry}>
          重试
        </button>
      </p>
    </div>
  )
}

/** 空态一：一条规则都没有。本体零内置源，只引导去设置页添加规则来源，不举例任何站点 */
function NoSourcesNotice({ data }: { data: SourcesData }) {
  const { errors } = data.rules
  return (
    <section className="search-empty" aria-labelledby="no-sources-heading">
      <h2 id="no-sources-heading" className="page-notice-title">
        还没有规则来源
      </h2>
      <p className="page-notice-copy">
        nagare 不内置任何磁力源。添加规则来源（规则仓库的 HTTPS 地址或本机目录）后即可搜索，
        搜索在你自己的电脑上执行。
      </p>
      {errors.length > 0 && (
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
      )}
      <p className="page-notice-actions">
        <Link to="/settings" className="hud-button hud-button--small search-empty-link">
          去设置添加规则来源
        </Link>
      </p>
    </section>
  )
}

/** 空态二：有规则但还没搜 */
function IdleNotice({ data }: { data: SourcesData }) {
  const total = data.sources.length
  const enabled = data.sources.filter((source) => source.enabled).length
  return (
    <section className="search-empty" aria-labelledby="idle-heading">
      <h2 id="idle-heading" className="page-notice-title">
        输入关键词开始搜索
      </h2>
      <p className="page-notice-copy">
        已加载 {total} 个源，{enabled} 个启用。结果可以直接「播放」（边下边播，交给 mpv），
        也可以复制磁力链接到别的下载器。
      </p>
    </section>
  )
}

/** 空态三：搜了但一条都没有 —— 文案按源状态分流，别让「源坏了」伪装成「没资源」 */
function EmptyResultsNotice({ query, outcomes }: { query: string; outcomes: SourceOutcome[] }) {
  const deadCount = outcomes.filter((outcome) => outcome.state === 'dead').length
  const failedCount = outcomes.filter((outcome) => outcome.state === 'failed').length
  const activeCount = outcomes.filter((outcome) => outcome.state !== 'disabled').length

  let title = `没有找到「${query}」的结果`
  let copy = '所有启用的源都返回了空结果，换个关键词试试。'
  if (deadCount > 0 || failedCount > 0) {
    title = '没有结果，且有源出了问题'
    copy = `${describeTrouble(deadCount, failedCount)}——把鼠标放到上方红色 / 橙色徽标上查看原因，规则损坏时请到设置页自检或重新同步。`
  } else if (activeCount === 0) {
    title = '所有源都已禁用'
    copy = '点击上方徽标启用至少一个源后会自动重新搜索。'
  }

  return (
    <section className="search-empty search-empty--results" aria-labelledby="empty-results-heading">
      <h2 id="empty-results-heading" className="page-notice-title">
        {title}
      </h2>
      <p className="page-notice-copy">{copy}</p>
    </section>
  )
}

function describeTrouble(deadCount: number, failedCount: number): string {
  const parts: string[] = []
  if (deadCount > 0) parts.push(`${deadCount} 个源异常（上游有条目但规则解析不出）`)
  if (failedCount > 0) parts.push(`${failedCount} 个源连接失败`)
  return parts.join('，')
}
