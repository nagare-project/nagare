import { FullPageNotice } from './FullPageNotice'

/**
 * 无有效 token 时的全页提示（根布局与各页面共用）。
 * token 只随启动链接下发（M0 决议），这里给出唯一可行的恢复动作。
 */
export function UnauthorizedNotice() {
  return (
    <FullPageNotice eyebrow="未授权" title="这个浏览器还没有访问凭证">
      nagare 只认带凭证的链接，直接输入地址打不开。点 Dock 图标，或菜单栏 / 托盘「流」图标里的「打开界面」（有 nagare 窗口时用「在浏览器中打开」），会用默认浏览器带凭证打开；想用别的浏览器，点同一菜单里的「复制登录链接」再粘贴到地址栏。
    </FullPageNotice>
  )
}
