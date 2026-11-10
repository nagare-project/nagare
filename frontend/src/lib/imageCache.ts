/**
 * 本页面里「已经加载成功过」的图片地址，以及横幅切换前的预加载。
 *
 * 封面淡入（MediaArtwork reveal）只该给真的要等的图：已经在内存里的图再挂载一次（换类型筛选、
 * 回到发现页）也从透明淡入，就是一次「黑一下再显示」。挂载时查这张表，命中就直接显示。
 */

/** 记多少个地址：目录里的封面与横幅加起来也就几百张，上限只防桌面窗口开几天无限增长 */
const MAX_REMEMBERED = 4096

const loaded = new Set<string>()
const inflight = new Map<string, Promise<boolean>>()

/** 这个地址在本页面里加载成功过 */
export function isArtworkLoaded(src: string): boolean {
  return loaded.has(src)
}

export function markArtworkLoaded(src: string) {
  // 先删再加：Set 按插入顺序迭代，最近用到的排到最后，超限时删最前面那个
  loaded.delete(src)
  loaded.add(src)
  if (loaded.size > MAX_REMEMBERED) loaded.delete(loaded.values().next().value as string)
}

/**
 * 预加载并解码一张图；成功 → true，失败 → false（不抛错）。
 * 同一个地址在途时复用同一次请求。解码失败（个别图 Safari 会拒绝 decode）仍按加载成功算：
 * 图已经在缓存里，挂上去时浏览器会自己解码。
 */
export function preloadArtwork(src: string): Promise<boolean> {
  if (loaded.has(src)) return Promise.resolve(true)
  const pending = inflight.get(src)
  if (pending) return pending
  const request = new Promise<boolean>((resolve) => {
    if (typeof Image === 'undefined') { resolve(false); return }
    const img = new Image()
    img.onload = () => {
      if (typeof img.decode !== 'function') { resolve(true); return }
      img.decode().then(() => resolve(true), () => resolve(true))
    }
    img.onerror = () => resolve(false)
    img.src = src
  }).then((ok) => {
    inflight.delete(src)
    if (ok) markArtworkLoaded(src)
    return ok
  })
  inflight.set(src, request)
  return request
}

/** 只给测试用：清掉记录，用例之间互不影响 */
export function resetArtworkCacheForTests() {
  loaded.clear()
  inflight.clear()
}
