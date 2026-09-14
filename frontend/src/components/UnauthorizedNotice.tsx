import { label } from '../theme'

/**
 * 无有效 token 时的全页提示（媒体库页与设置页共用）。
 * token 只随启动链接下发（M0 决议），这里给出唯一可行的恢复动作。
 */
export function UnauthorizedNotice() {
  return (
    <main className="lib-shell">
      <div className="page-notice">
        <p className="panel-heading" style={label}>
          unauthorized
        </p>
        <h1 className="page-notice-title">这个浏览器还没有访问凭证</h1>
        <p className="page-notice-copy">
          nagare 只认带凭证的链接，直接输入地址打不开。从 Dock / 菜单栏 / 托盘的「流」图标点
          「打开界面」会用默认浏览器带凭证打开；想用别的浏览器，点同一菜单里的
          「复制登录链接」再粘贴到地址栏。
        </p>
      </div>
    </main>
  )
}
