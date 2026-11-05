import { useEffect, useRef } from 'react'
import { Link } from '@tanstack/react-router'
import type { MouseEvent } from 'react'
import { Icon } from '../../components/ui/Icon'
import { SETTINGS_GROUPS } from './sections'
import type { SettingsSectionId } from './sections'

export interface SettingsNavProps {
  /** 当前打开的分区 */
  current: SettingsSectionId
  /** 用户点了某个分区（普通左键）：页面据此在切换后把焦点移到那一区的标题上 */
  onNavigate: (id: SettingsSectionId) => void
}

/** 新标签页打开、带修饰键的点击不切换本页的分区 */
function isPlainClick(event: MouseEvent): boolean {
  return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey
}

/**
 * 设置分类导航。每组是一个以组名命名的列表；分区链接改的是地址里的 #hash，可以收藏、分享、后退。
 *
 * includeHash 不能省：不带它时 TanStack 只比路径，同一路径下的每个分区都会被标成当前页。
 * 没有 #hash 时默认分区要自己标 aria-current —— 那时没有哪条链接的 hash 与地址相同。
 */
export function SettingsNav({ current, onNavigate }: SettingsNavProps) {
  const nav = useRef<HTMLElement>(null)

  // 窄屏上导航是横向滚动的一行：当前分区可能在屏幕外（从别处直达 #about），把它滚进来。
  // 只动导航自己的 scrollLeft，不用 scrollIntoView —— 那会连带整页上下滚动。
  useEffect(() => {
    const element = nav.current
    if (element === null || element.scrollWidth <= element.clientWidth) return
    const link = element.querySelector<HTMLElement>('[aria-current="page"]')
    if (link === null) return
    const left = link.offsetLeft - element.clientWidth / 2 + link.offsetWidth / 2
    element.scrollLeft = Math.max(0, left)
  }, [current])

  return (
    <nav ref={nav} className="settings-nav" aria-label="设置分类">
      {SETTINGS_GROUPS.map((group) => {
        const labelId = group.label === undefined ? undefined : `settings-nav-${group.key}`
        return (
          <div key={group.key} className="settings-nav-group">
            {group.label !== undefined && (
              <p id={labelId} className="settings-nav-label">
                {group.label}
              </p>
            )}
            <ul className="settings-nav-list" aria-labelledby={labelId}>
              {group.items.map((item) => (
                <li key={item.label}>
                  {item.kind === 'page' ? (
                    <Link to={item.to} className="settings-nav-item">
                      <Icon name={item.icon} size={18} />
                      <span className="settings-nav-text">{item.label}</span>
                      <Icon name="right" size={16} style={{ marginLeft: 'auto' }} />
                    </Link>
                  ) : (
                    <Link
                      to="/settings"
                      hash={item.id}
                      className="settings-nav-item"
                      aria-current={current === item.id ? 'page' : undefined}
                      activeOptions={{ includeHash: true }}
                      onClick={(event) => {
                        if (isPlainClick(event)) onNavigate(item.id)
                      }}
                    >
                      <Icon name={item.icon} size={18} />
                      <span className="settings-nav-text">{item.label}</span>
                    </Link>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )
      })}
    </nav>
  )
}
