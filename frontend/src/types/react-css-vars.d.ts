/**
 * 允许在 React 的 style 属性里写 CSS 自定义属性（--xxx）。
 * tokens.ts 生成的调色值就是以 CSS 变量注入组件树的（见 HomePage 的 palette）。
 */
import 'react'

declare module 'react' {
  interface CSSProperties {
    [variable: `--${string}`]: string | number | undefined
  }
}
