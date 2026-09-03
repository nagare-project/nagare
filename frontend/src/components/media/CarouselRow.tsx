import type { ReactNode } from 'react'

/**
 * 一个带标题的横向滚动行（发现页的每个板块）。
 *
 * 不做左右箭头按钮：触控板与触屏都能直接横向滚，而箭头在这两种输入下是死重量。
 * 键盘可达性靠卡片本身的 tab 顺序，不靠额外控件。
 */
export function CarouselRow({
  title,
  subtitle,
  children,
}: {
  title: string
  subtitle?: string
  children: ReactNode
}) {
  return (
    <section className="row" aria-label={title}>
      <div className="row-head">
        <h2 className="row-title">{title}</h2>
        {subtitle !== undefined && <p className="row-sub">{subtitle}</p>}
      </div>
      <ul className="row-scroll">{children}</ul>
    </section>
  )
}
