/** sessionStorage 中保存 token 的键名 */
export const TOKEN_STORAGE_KEY = 'nagare_token'

/** 启动链接携带 token 的 query 参数名 */
export const TOKEN_QUERY_PARAM = 'token'

/**
 * 后端 token 的固定形状：128 位随机数的 32 个小写十六进制字符。
 * 形状校验挡住的不是"猜对 token"（不可能），而是垃圾值毒化会话：
 * 比如 ?token=abc%0D%0Adef 解码后带控制字符，一旦存进 sessionStorage，
 * 之后每次 Headers.set 都会同步抛错，这个标签页的所有 API 调用全部报废。
 */
const TOKEN_PATTERN = /^[0-9a-f]{32}$/

/** 判断候选值是否符合 token 形状（null 安全的类型收窄）。 */
export function isValidToken(candidate: string | null): candidate is string {
  return candidate !== null && TOKEN_PATTERN.test(candidate)
}

/**
 * 从 query string 中解析 token 参数。
 *
 * 纯函数：不碰任何浏览器全局（window / sessionStorage），
 * 因此可以在 vitest 的 node 环境里直接单测。
 * 接受带或不带前导 "?" 的输入；参数缺失、为空串或不符合
 * 32 位小写 hex 形状时一律返回 null。
 */
export function parseTokenFromSearch(search: string): string | null {
  const token = new URLSearchParams(search).get(TOKEN_QUERY_PARAM)
  return isValidToken(token) ? token : null
}

/**
 * 获取当前会话的 API token。
 *
 * 后端启动时会自动打开 `http://127.0.0.1:<port>/?token=<32位hex>`，约定如下：
 * 1. URL 里带合法 token → 存入 sessionStorage 并返回该值；
 * 2. URL 里没有 → 回读 sessionStorage（同一标签页里刷新后仍然可用）；
 *    存的值形状不对（旧版本残留/外部写入）就清掉，当作没有；
 * 3. 两处都没有 → 返回 null，由调用方提示用户通过启动链接重新访问。
 *
 * 只要地址栏出现过 token 参数——无论值合不合法——都会用 replaceState 抹掉
 * （防止截图、浏览历史、复制分享链接把 token 泄露出去）。
 */
export function acquireToken(): string | null {
  const search = window.location.search
  const fromUrl = parseTokenFromSearch(search)
  if (new URLSearchParams(search).has(TOKEN_QUERY_PARAM)) {
    scrubTokenFromAddressBar()
  }
  if (fromUrl !== null) {
    window.sessionStorage.setItem(TOKEN_STORAGE_KEY, fromUrl)
    return fromUrl
  }

  const stored = window.sessionStorage.getItem(TOKEN_STORAGE_KEY)
  if (stored !== null && !isValidToken(stored)) {
    window.sessionStorage.removeItem(TOKEN_STORAGE_KEY)
    return null
  }
  return stored
}

/**
 * 把 token 参数从地址栏抹掉。
 * 保留其余 query 参数与 hash，沿用当前 history state，不新增历史记录。
 */
function scrubTokenFromAddressBar(): void {
  const url = new URL(window.location.href)
  url.searchParams.delete(TOKEN_QUERY_PARAM)
  window.history.replaceState(window.history.state, '', url.toString())
}
