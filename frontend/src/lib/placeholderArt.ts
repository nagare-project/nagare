/**
 * 假封面：由标题算出的确定性渐变。
 *
 * 为什么不用图片占位服务：CSP 是 `img-src 'self' data:`（见 httpserver/middleware.go），
 * 外部图一律被挡；而且假数据引外部请求本身就不对 —— 界面演示不该联网。
 * 渐变离线可用、每部番颜色稳定（同一标题永远同一色），
 * 而且一眼看得出是占位，不会被误当成真封面。
 *
 * 住在 lib/ 而不是 lib/fixtures/：它不是假数据。真实作品同样会没有封面
 *（animego 只在播放前匹配到才有图），那时照样要用这个渐变兜底。
 */

/** FNV-1a，够用的字符串散列（与色相映射，不做密码学用途） */
function hash(s: string): number {
  let h = 0x811c9dc5
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 0x01000193) >>> 0
  }
  return h
}

/** 由标题生成一段 CSS 渐变，直接赋给 background */
export function placeholderArt(title: string): string {
  const h = hash(title)
  const hue = h % 360
  const hue2 = (hue + 40 + (h >> 9) % 60) % 360
  return `linear-gradient(150deg, oklch(38% 0.09 ${hue}) 0%, oklch(20% 0.06 ${hue2}) 70%)`
}
