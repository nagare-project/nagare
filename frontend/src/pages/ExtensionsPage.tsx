import { SourcesCard } from '../components/search/SourcesCard'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useSources } from '../hooks/useSources'
import '../components/library/library.css'
import '../components/media/media.css'

/**
 * `/extensions` 扩展 —— 对应 seanime 的同名页面。
 *
 * 这一页【没有假数据】：nagare 的规则系统是真的，之前只是埋在设置页里。
 * 提成一等页面之后设置页那份仍然保留（改配置的人会在设置里找它）。
 *
 * 与 seanime 的差别不是没做完，是两条不同的路，值得对从那边过来的人讲清楚：
 * 它的扩展是 JS 插件，装进去在 goja VM 里跑；nagare 的是声明式 YAML，
 * 只能「发一个 GET、按路径取字段、做几步固定转换」，没有脚本、没有沙箱、
 * 也就没有沙箱要防的东西。
 */
export function ExtensionsPage() {
  const sources = useSources()

  if (sources.state.phase === 'unauthorized') return <UnauthorizedNotice />

  return (
    <main className="lib-shell">
      <header className="page-head">
        <h1 className="page-title">扩展</h1>
      </header>

      <section className="panel ext-intro">
        <h2 className="panel-heading">规则，不是插件</h2>
        <p className="page-notice-copy">
          nagare 的扩展是<strong>声明式 YAML 规则</strong>：一条规则只能描述
          「往哪发一个 GET、按路径取哪些字段、做几步固定的转换」。
          没有脚本、没有代码执行 —— 所以装一条规则不需要你审计它能干什么，
          它能干的事上限就写在格式里。
        </p>
        <p className="page-notice-copy">
          代价是表达力有上限；好处是<strong>装第三方规则的风险与装一个 JSON 配置相同</strong>。
          规则地址由你自己填，nagare 不预填、不内置、不推荐任何来源。
        </p>
      </section>

      <SourcesCard sources={sources} />
    </main>
  )
}
