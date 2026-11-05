import type { ReactNode } from 'react'

/**
 * 徽标。语义色一律走 tone，不要在调用处拼 `badge--warn`。
 *
 * 状态色规则（DESIGN.md 颜色规则 6）：语义色文字 + 同色 12% 底。
 * 这条规则封在 CSS 里，这一层只负责把语义翻成类名。
 */
export type BadgeTone = 'neutral' | 'accent' | 'warn'

export interface BadgeProps {
  tone?: BadgeTone
  children: ReactNode
  title?: string
  extraClass?: string
}

const TONE_CLASS: Record<BadgeTone, string> = {
  neutral: '',
  accent: 'badge--accent',
  warn: 'badge--warn',
}

export function Badge({ tone = 'neutral', children, title, extraClass }: BadgeProps) {
  const className = ['badge', TONE_CLASS[tone], extraClass ?? ''].filter(Boolean).join(' ')
  return (
    <span className={className} title={title}>
      {children}
    </span>
  )
}
