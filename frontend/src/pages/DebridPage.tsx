import { useState } from 'react'
import { label } from '../theme'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/debrid` Debrid 服务 —— 对应 seanime 的同名页面。
 *
 * FIXME(G7): 界面预览，后端没有对接。Debrid 是把种子在服务商那边下好、
 * 再给你一条 HTTP 直链的付费中间服务（Real-Debrid、AllDebrid 等）。
 *
 * ⚠️ 接之前要想清楚一件事，它不是技术问题：调研里「真正的杀伤在分发渠道与
 * 中间服务」那一条，举的例子正是 Real-Debrid 掐掉第三方客户端的 API。
 * 把播放链路挂到一个可以单方面掐断的第三方上，与 nagare「本地优先、
 * 断网也能看本地文件」的取向是相反的。缺口与利弊见仓库根 todos.md。
 */
export function DebridPage() {
  const [key, setKey] = useState('')
  const [saved, setSaved] = useState(false)

  return (
    <main className="lib-shell">
      <header className="page-head">
        <h1 className="page-title">Debrid 服务</h1>
      </header>

      <p className="alert-warn" role="status">
        <strong>这一页只是界面预览，后端没有对接。</strong>
        填了密钥也不会生效。缺口 G7，见仓库根 todos.md。
      </p>

      <section className="panel settings-card">
        <h2 className="panel-heading">API 密钥</h2>
        <p className="page-notice-copy">
          Debrid 是第三方付费服务：种子在它那边下好，再给你一条 HTTP 直链。
          它能省掉本机的下载与做种，代价是<strong>播放链路多一个可以单方面掐断的中间人</strong>。
        </p>

        <label htmlFor="debrid-key" style={label}>
          密钥
        </label>
        <input
          id="debrid-key"
          className="input"
          type="password"
          autoComplete="off"
          placeholder="粘贴你的 API 密钥"
          value={key}
          onChange={(e) => {
            setKey(e.target.value)
            setSaved(false)
          }}
        />

        <p>
          <button
            type="button"
            className="btn btn--sm"
            disabled={key.trim() === ''}
            onClick={() => setSaved(true)}
          >
            保存
          </button>
        </p>

        <p className="result result--warn" role="status" aria-live="polite">
          {saved ? '没有保存 —— 这一页还没有后端。' : ''}
        </p>
      </section>
    </main>
  )
}
