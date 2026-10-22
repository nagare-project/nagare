/**
 * 这个页面开在哪里：nagare 自己的桌面窗口，还是外部浏览器的标签页（托盘「在浏览器中打开」、复制登录链接）。
 * 两边载入的是同一个网页、同一套鉴权，只有提示文案不一样（「关闭窗口」还是「关闭标签页」）。
 *
 * 桌面窗口的宿主（macOS 的 WKWebView、Windows 的 WebView2）在 User-Agent 末尾追加一个记号，
 * 约定见 docs/player-ui.md「宿主与网页」。这只决定措辞，不当作任何权限判断：外部浏览器照抄这个记号
 * 也只会看到另一句提示。
 */

/** 桌面窗口追加在 User-Agent 末尾的记号（可以带 `/<版本>`） */
export const DESKTOP_UA_MARKER = 'NagareDesktop'

/** 这个 User-Agent 是不是 nagare 的桌面窗口。按空白分词整词比较，不认「NagareDesktopX」这类前缀撞车 */
export function isDesktopWindowUA(userAgent: string): boolean {
  return userAgent
    .split(/\s+/)
    .some((token) => token === DESKTOP_UA_MARKER || token.startsWith(`${DESKTOP_UA_MARKER}/`))
}

/** 当前页面是不是开在 nagare 的桌面窗口里 */
export function inDesktopWindow(): boolean {
  if (typeof navigator === 'undefined' || typeof navigator.userAgent !== 'string') {
    return false
  }
  return isDesktopWindowUA(navigator.userAgent)
}
