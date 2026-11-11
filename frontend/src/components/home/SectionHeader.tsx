import type { ReactNode } from 'react'
import { RouteLink } from '../catalog/RouteLink'
import { ChevronIcon } from './icons'
import styles from './section.module.css'

// 发现页每个板块共用的标题行：一小段作品色短线、标题、可选的数字与说明、右边可选的链接。

interface SectionHeaderProps {
  id: string
  title: string
  /** 标题旁的数字（等宽） */
  count?: number
  /** 标题后面一句安静的说明：数据来源、日期、分组规则 */
  note?: ReactNode
  link?: { to: string; search?: Record<string, unknown>; label: string }
  /** 右边的其他东西，比如一个按钮 */
  action?: ReactNode
  /** 手机版面放不下、要省掉的部分 */
  phoneHides?: ReadonlyArray<'note' | 'link'>
}

export function SectionHeader({ id, title, count, note, link, action, phoneHides = [] }: SectionHeaderProps) {
  const hide = (part: 'note' | 'link') => (phoneHides.includes(part) ? ` ${styles.desktopOnly}` : '')
  return (
    <div className={styles.head}>
      <span className={styles.rule} aria-hidden="true" />
      <h2 id={id} className={styles.title}>
        {title}
      </h2>
      {typeof count === 'number' ? <span className={styles.count}>{count}</span> : null}
      {note ? <span className={`${styles.note}${hide('note')}`}>{note}</span> : null}
      <span className={styles.spacer} />
      {action}
      {link ? (
        <RouteLink to={link.to} search={link.search} className={`${styles.link}${hide('link')}`}>
          {link.label}
          <ChevronIcon className={styles.linkIcon} />
        </RouteLink>
      ) : null}
    </div>
  )
}
