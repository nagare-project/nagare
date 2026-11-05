import type { ReactNode } from 'react'
import './catalog/catalog.css'
import './full-page-notice.css'

export interface FullPageNoticeProps {
  /** 标题上方的一个小词（「未授权」「已退出」），只读信息 */
  eyebrow?: string
  title: string
  children?: ReactNode
  actions?: ReactNode
}

/**
 * 整页只剩一句话的时候（没有访问凭证、nagare 已退出）：页面主标题 + 说明 + 动作，
 * 版式与目录页的整块状态说明一致（DESIGN.md「状态表」），自己就是这一页的 <main>。
 */
export function FullPageNotice({ eyebrow, title, children, actions }: FullPageNoticeProps) {
  return (
    <main className="catalog-page">
      <div className="catalog-notice">
        {eyebrow !== undefined && <p className="full-page-notice-eyebrow">{eyebrow}</p>}
        <h1 className="catalog-notice-title full-page-notice-title">{title}</h1>
        {children !== undefined && <p className="catalog-notice-body">{children}</p>}
        {actions !== undefined && <div className="catalog-notice-actions">{actions}</div>}
      </div>
    </main>
  )
}
