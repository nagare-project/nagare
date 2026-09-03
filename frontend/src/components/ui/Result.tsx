import type { ReactNode } from 'react'

/**
 * 状态行：操作结果、错误、提示。全仓 44 处，是重复最多的一个。
 *
 * 两个容易漏、而且漏了只在特定情形才暴露的细节，封在这里一次做对：
 *
 *  1. `.result` 有 `min-height`，即使没内容也占位 —— 结果出现/消失时
 *     不会把下面的东西推上推下。
 *  2. 报错这类内容是异步冒出来的，读屏必须被告知。所以 tone 为 err/warn 时
 *     默认挂 role="status" + aria-live="polite"；手写时这一步几乎总是被忘掉。
 */
export type ResultTone = 'dim' | 'ok' | 'warn' | 'err'

export interface ResultProps {
  tone?: ResultTone
  children?: ReactNode
  /** 数字读数场景传 theme 的 mono；纯文字不用传 */
  style?: React.CSSProperties
  extraClass?: string
  /** 覆盖默认的 live region 判定（极少用到） */
  live?: boolean
}

export function Result({ tone = 'dim', children, style, extraClass, live }: ResultProps) {
  const className = ['result', `result--${tone}`, extraClass ?? ''].filter(Boolean).join(' ')
  const announce = live ?? (tone === 'err' || tone === 'warn')
  return (
    <p
      className={className}
      style={style}
      role={announce ? 'status' : undefined}
      aria-live={announce ? 'polite' : undefined}
    >
      {children}
    </p>
  )
}
