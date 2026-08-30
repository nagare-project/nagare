import { hudPalette } from '../lib/palette'
import { label } from '../tokens'

/**
 * 无有效 token 时的全页提示（媒体库页与设置页共用）。
 * token 只随启动链接下发（M0 决议），这里给出唯一可行的恢复动作。
 */
export function UnauthorizedNotice() {
  return (
    <main className="lib-shell" style={hudPalette}>
      <div className="page-notice">
        <p className="panel-heading" style={label}>
          unauthorized
        </p>
        <h1 className="page-notice-title">未携带有效 token</h1>
        <p className="page-notice-copy">
          请通过 nagare 启动时自动打开的浏览器链接访问（链接中带有本次会话的访问凭证）。
        </p>
      </div>
    </main>
  )
}
