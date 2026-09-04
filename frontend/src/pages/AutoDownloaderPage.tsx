import { useState } from 'react'
import { FAKE_RULES } from '../lib/fixtures/scans'
import type { FakeRule } from '../lib/fixtures/scans'
import { mono } from '../theme'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/auto-downloader` 自动下载。
 *
 * FIXME(G6): 这一页是【界面预览】，背后什么都没有 —— nagare 没有订阅、
 * 没有定时轮询、没有自动下载。`internal/rules` 是【搜索】用的规则引擎，
 * 不是订阅器。整套东西是一个里程碑级功能。
 *
 * 提示语措辞刻意比别的页面重：其他假数据页至少形状是真的（列表就是列表），
 * 而这一页点「启用」之后【永远不会有任何东西被下载】。说成「假数据」
 * 会让人以为只是数字不准。
 *
 * 红线 1：源必须由用户提供。所以示例地址写成 `<你自己的规则源>` 占位，
 * 不预填任何可用的 RSS —— 预填等于官方分发源。
 */
export function AutoDownloaderPage() {
  const [rules, setRules] = useState<FakeRule[]>(FAKE_RULES)

  function toggle(id: string): void {
    setRules((prev) => prev.map((r) => (r.id === id ? { ...r, enabled: !r.enabled } : r)))
  }

  return (
    <main className="lib-shell">
      <header className="page-head">
        <h1 className="page-title">自动下载</h1>
      </header>

      <p className="alert-warn" role="status">
        <strong>这一页只是界面预览，功能尚未实现。</strong>
        nagare 目前没有订阅、没有定时轮询，
        <strong>点「启用」不会下载任何东西</strong>（缺口 G6）。
      </p>

      <ul className="rule-list">
        {rules.map((r) => (
          <li key={r.id} className="panel rule">
            <div className="rule-main">
              <p className="rule-title">{r.title}</p>
              <p className="rule-feed" style={mono}>
                {r.feed}
              </p>
              <p className="rule-meta">
                {r.quality} · 已匹配 {r.matched} 集 ·{' '}
                {r.lastCheckedAt === null
                  ? '从未检查'
                  : `上次检查 ${new Date(r.lastCheckedAt).toLocaleString('zh-CN', { hour12: false })}`}
              </p>
            </div>
            <button
              type="button"
              className="switch"
              role="switch"
              aria-checked={r.enabled}
              aria-label={`${r.enabled ? '停用' : '启用'} ${r.title} 的订阅`}
              onClick={() => toggle(r.id)}
            >
              <span className="switch-knob" />
            </button>
          </li>
        ))}
      </ul>

      <p className="result result--dim">
        规则地址一律由你自己填写 —— nagare 不预填、不内置、不推荐任何订阅源。
      </p>
    </main>
  )
}
