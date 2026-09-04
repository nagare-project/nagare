import type { CSSProperties } from 'react'

/**
 * nagare 的设计 token（内联样式那一半；颜色与原子件在 styles/global.css）。
 *
 * 设计语言参照 seanime —— 内容优先、chrome 退让的媒体库应用外观：
 * 近黑三层表面、发丝白描边、状态色在暗底上一律取浅档，品牌色克制使用。
 * 排版、尺寸和组件结构与本地 seanime 参考实现对齐。
 *
 * 这里只放【必须以内联样式表达】的东西：跨组件复用、又不值得为它起一个类名的
 * 排版组合。结构性样式一律走 CSS 类，不要往这里加。
 */

/**
 * mono —— 数字读数专用（速度、体积、时间码、分享者数）。
 *
 * 保留等宽不是装饰：这些数字每秒都在跳，比例字体下字宽变化会让整行左右抖动。
 * tabular-nums + 等宽栈把每一位钉死在同一宽度上。
 * 字体栈用系统等宽，不再指定 JetBrains Mono —— 用户机器上大概率没有，
 * 指定了只会白白多一次字体回退。
 */
export const mono = {
  fontFamily:
    'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
  fontVariantNumeric: 'tabular-nums',
} as const satisfies CSSProperties

/**
 * label —— 次要说明文字（字段名、单位、状态注解）。
 *
 * 从 HUD 时期的「10px 全大写 + 0.08em 字距」改成常规小字：
 * 全大写对中文无效（中文没有大小写），却会把英文字段名拉长、并降低可读性 ——
 * 一个中英混排的界面里，全大写标签只在英文那半边生效，视觉上是割裂的。
 */
export const label = {
  fontSize: '0.84rem',
  lineHeight: 1.5,
  color: 'var(--fg-muted)',
} as const satisfies CSSProperties
