import { useId } from 'react'
import type { ReactNode } from 'react'

/**
 * 面板卡。全仓 28 处。
 *
 * 封起来主要为了一件手写时常漏的事：**面板要和它的标题关联起来**。
 * 这里用 useId 生成标题 id 并自动接上 aria-labelledby —— 读屏用户
 * 进入这个区块时会先听到它是什么，而不是一段没有上下文的内容。
 */
export interface PanelProps {
  /** 标题；不传则渲染成无标题的纯容器（此时也不会加 aria-labelledby） */
  heading?: ReactNode
  children: ReactNode
  /** 面板专属的布局类，如 settings-card / torrent-task */
  extraClass?: string
  /** 标题右侧的动作区（停止、重新检测这类） */
  action?: ReactNode
}

export function Panel({ heading, children, extraClass, action }: PanelProps) {
  const headingId = useId()
  const className = ['panel', extraClass ?? ''].filter(Boolean).join(' ')

  return (
    <section className={className} aria-labelledby={heading === undefined ? undefined : headingId}>
      {heading !== undefined &&
        (action === undefined ? (
          <h2 id={headingId} className="panel-heading">
            {heading}
          </h2>
        ) : (
          <header className="panel-head">
            <h2 id={headingId} className="panel-heading">
              {heading}
            </h2>
            {action}
          </header>
        ))}
      {children}
    </section>
  )
}
