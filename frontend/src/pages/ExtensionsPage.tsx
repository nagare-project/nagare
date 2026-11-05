import { RouteLink } from '../components/catalog/RouteLink'
import { SourcesCard } from '../components/search/SourcesCard'
import { Icon } from '../components/ui/Icon'
import { UnauthorizedNotice } from '../components/UnauthorizedNotice'
import { useSources } from '../hooks/useSources'
import '../components/catalog/catalog.css'
import '../styles/panels.css'

/**
 * `/extensions` 扩展：nagare 的声明式规则系统（真数据），入口在设置的分类导航里。
 * 设置「来源」里那份仍然保留（改配置的人会在设置里找它），这一页多讲清楚「规则不是插件」。
 *
 * 与可执行插件的差别不是没做完，是两条不同的路，值得对从别处过来的人讲清楚：
 * 那边的扩展是脚本，装进去在虚拟机里跑；nagare 的是声明式 YAML，
 * 只能「发一个 GET、按路径取字段、做几步固定转换」，没有脚本、没有沙箱、
 * 也就没有沙箱要防的东西。
 */
export function ExtensionsPage() {
  const sources = useSources()

  if (sources.state.phase === 'unauthorized') return <UnauthorizedNotice />

  return (
    <main className="catalog-page page-narrow">
      {/* 这一页讲的就是来源规则：回去落在设置的「来源」分区，而不是默认的账号 */}
      <RouteLink to="/settings#sources" className="page-back">
        <Icon name="left" size={18} />
        设置
      </RouteLink>
      <header className="catalog-head">
        <div>
          <h1 className="catalog-title">扩展</h1>
          <p className="catalog-subline">磁力来源由你自己提供的规则描述；nagare 不预填、不内置、不推荐任何来源。</p>
        </div>
      </header>

      <section className="info-panel" aria-labelledby="ext-intro-heading">
        <h2 id="ext-intro-heading" className="info-panel-title">
          规则，不是插件
        </h2>
        <p className="info-panel-text">
          nagare 的扩展是<strong>声明式 YAML 规则</strong>：一条规则只能描述「往哪发一个 GET、按路径取哪些字段、做几步固定的转换」。没有脚本、没有代码执行 —— 所以装一条规则不需要你审计它能干什么，它能干的事上限就写在格式里。
        </p>
        <p className="info-panel-text">
          代价是表达力有上限；好处是<strong>装第三方规则的风险与装一个 JSON 配置相同</strong>。规则地址由你自己填，nagare 不预填、不内置、不推荐任何来源。
        </p>
      </section>

      <SourcesCard sources={sources} headingLevel={2} />
    </main>
  )
}
