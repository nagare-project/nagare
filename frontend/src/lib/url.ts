/**
 * 只把 http(s) 链接当成可点的外链。
 * 规则文件是用户提供的、更新地址来自后端转述的 GitHub 响应 —— 任何一处都不该让
 * `javascript:` 之类变成可点击的 <a>。
 */
const HTTP_URL = /^https?:\/\//i

export function isHttpUrl(url: string): boolean {
  return HTTP_URL.test(url)
}
