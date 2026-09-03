import type { ButtonHTMLAttributes, ReactNode } from 'react'

/**
 * 按钮。
 *
 * 为什么要这一层：改造前全仓 42 处直接写 `className="btn btn--sm"`，
 * 意图（这是主动作 / 这是危险动作）散在字符串里，加一档样式要全仓 grep。
 *
 * 类名与手写时期【逐字一致】—— 样式表一行没改，测试也不用改。
 * 这一层买的是意图可读与改一处生效，不是换一套 CSS。
 */

/** 意图决定视觉，不要在调用处直接拼 `btn--primary` */
export type ButtonIntent =
  /** 次要动作：透明底 + 发丝边。默认档，因为一屏上大部分按钮都不是主动作 */
  | 'secondary'
  /** 主动作：实心品牌色。一屏至多一个 */
  | 'primary'
  /** 危险动作：平时收敛成次要，hover 才转红 —— 常驻红色会让人不敢碰旁边的东西 */
  | 'danger'

export interface ButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'className'> {
  intent?: ButtonIntent
  size?: 'md' | 'sm'
  children: ReactNode
  /** 逃生口：确实需要额外定位类时用，不要拿它来改视觉 */
  extraClass?: string
}

const INTENT_CLASS: Record<ButtonIntent, string> = {
  secondary: '',
  primary: 'btn--primary',
  danger: 'btn--danger',
}

export function Button({
  intent = 'secondary',
  size = 'md',
  extraClass,
  type = 'button',
  children,
  ...rest
}: ButtonProps) {
  const className = ['btn', size === 'sm' ? 'btn--sm' : '', INTENT_CLASS[intent], extraClass ?? '']
    .filter(Boolean)
    .join(' ')
  // type 默认 button：表单里漏写 type 的 <button> 会变成提交按钮，
  // 那是最容易被忽略、又最容易出事的一个默认值。
  return (
    <button type={type} className={className} {...rest}>
      {children}
    </button>
  )
}
