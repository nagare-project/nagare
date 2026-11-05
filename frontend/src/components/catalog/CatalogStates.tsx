import type { ReactNode } from 'react'

/**
 * 整块的状态说明（DESIGN.md「状态表」里的空 / 出错）：标题 + 一句说明 + 动作。
 * tone="error" 时标题与说明外面包一层 role="alert"，读屏马上念出原因；不挂在标题自己身上 ——
 * 那样标题就不再是标题，按标题跳转的读屏用户找不到这一块。动作按钮不在播报里。
 */
export function CatalogNotice({ title, children, actions, tone = 'empty' }: {
  title: string
  children?: ReactNode
  actions?: ReactNode
  tone?: 'empty' | 'error'
}) {
  return (
    <section className="catalog-notice" data-tone={tone}>
      <div className="catalog-notice-text" role={tone === 'error' ? 'alert' : undefined}>
        <h2 className="catalog-notice-title">{title}</h2>
        {children && <p className="catalog-notice-body">{children}</p>}
      </div>
      {actions && <div className="catalog-notice-actions">{actions}</div>}
    </section>
  )
}

/** 海报网格的静态骨架（不闪烁：DESIGN.md 状态表写的是「静态骨架」） */
export function PosterSkeletons({ count = 12, label }: { count?: number; label: string }) {
  return (
    <div role="status" aria-busy="true">
      <span className="visually-hidden">{label}</span>
      <ul className="catalog-grid" aria-hidden="true">
        {Array.from({ length: count }, (_, i) => (
          <li key={i} className="poster-skeleton">
            <span className="poster-skeleton-art" />
            <span className="poster-skeleton-line" />
            <span className="poster-skeleton-line poster-skeleton-line--short" />
          </li>
        ))}
      </ul>
    </div>
  )
}
