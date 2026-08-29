import { useState } from 'react'
import type { CSSProperties } from 'react'
import { ApiAuthError, ApiError, fetchHealth } from '../lib/api'
import { HUE, label, mono, oklchToken } from '../tokens'

/**
 * 页面点缀色全部由 tokens.ts 生成：LIVE Cyan（HUE.s07 = 195）按层级表分档。
 * 以 CSS 变量注入 <main>，具体用法见 styles/global.css。
 */
const palette = {
  '--hud-border': oklchToken('rail', HUE.s07, 0.35),
  '--hud-border-dim': oklchToken('rail', HUE.s07, 0.16),
  '--hud-accent': oklchToken('readout', HUE.s07),
  '--hud-accent-hot': oklchToken('hot', HUE.s07),
  '--hud-flash': oklchToken('flash', HUE.s07, 0.12),
} satisfies CSSProperties

/** 「检查连接」按钮的状态机 */
type CheckState =
  | { phase: 'idle' }
  | { phase: 'checking' }
  | { phase: 'ok'; version: string }
  | { phase: 'unauthorized' }
  | { phase: 'failed'; message: string }

/** 把状态机映射成结果行的文案与语气（颜色语义见 global.css 的 .result--*） */
function resultView(check: CheckState): { tone: 'dim' | 'ok' | 'warn' | 'err'; text: string } | null {
  switch (check.phase) {
    case 'idle':
      return null
    case 'checking':
      return { tone: 'dim', text: '正在请求 /api/health …' }
    case 'ok':
      return { tone: 'ok', text: `鉴权链路正常 · 后端 v${check.version}` }
    case 'unauthorized':
      return { tone: 'warn', text: '未携带有效 token，请通过启动时自动打开的链接访问' }
    case 'failed':
      return { tone: 'err', text: check.message }
  }
}

export function HomePage() {
  const [check, setCheck] = useState<CheckState>({ phase: 'idle' })

  async function handleCheck(): Promise<void> {
    setCheck({ phase: 'checking' })
    try {
      const health = await fetchHealth()
      setCheck({ phase: 'ok', version: health.version })
    } catch (err) {
      if (err instanceof ApiAuthError) {
        setCheck({ phase: 'unauthorized' })
        return
      }
      // 错误不静默：非鉴权类失败在控制台留全量上下文，页面上给可读文案
      console.error('健康检查失败', err)
      const message =
        err instanceof ApiError ? err.message : '发生未知错误，详情见浏览器控制台'
      setCheck({ phase: 'failed', message })
    }
  }

  const view = resultView(check)

  return (
    <main className="shell" style={palette}>
      <header className="masthead">
        <p className="masthead-eyebrow" style={label}>
          nagare · local anime agent
        </p>
        <h1 className="masthead-title">
          nagare
          <span className="masthead-kana" aria-hidden="true">
            流れ
          </span>
        </h1>
        <p className="masthead-tagline">
          本地动漫播放 agent —— M0 骨架已就位，下一步是与后端完成第一次鉴权握手。
        </p>
      </header>

      <section className="panel" aria-labelledby="health-heading">
        <h2 id="health-heading" className="panel-heading" style={label}>
          connection check
        </h2>
        <p className="panel-copy">
          点击按钮向本机 agent 发起 <code style={mono}>GET /api/health</code>
          ，验证「浏览器 → 鉴权中间件 → 后端」这条链路是否走通。
        </p>
        <div className="panel-actions">
          <button
            type="button"
            className="check-button"
            style={mono}
            onClick={() => void handleCheck()}
            disabled={check.phase === 'checking'}
          >
            {check.phase === 'checking' ? '连接中 …' : '检查连接'}
          </button>
          {/* 结果行常驻占位（min-height 见 CSS），避免内容出现时布局跳动 */}
          <p
            className={view === null ? 'result' : `result result--${view.tone}`}
            style={mono}
            role="status"
            aria-live="polite"
          >
            {view?.text}
          </p>
        </div>
      </section>

      <footer className="colophon" style={label}>
        M0 · skeleton &amp; auth
      </footer>
    </main>
  )
}
