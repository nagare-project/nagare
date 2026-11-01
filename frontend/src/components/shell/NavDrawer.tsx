import { useEffect, useRef } from 'react'
import type { MouseEvent, RefObject } from 'react'
import { Link, useLocation } from '@tanstack/react-router'
import { Icon } from '../ui/Icon'
import { isBackdropEvent } from '../../lib/dialog'
import { PRIMARY_NAV, navActiveOptions } from './navItems'

/**
 * 抽屉所在的窄屏范围：与 navigation.css 里的 (max-width: 1023px) 是同一个查询（DESIGN.md 的 --bp-nav）。
 * 写成同一句而不是 min-width: 1024px：缩放后的小数宽度（如 1023.4px）两句都不命中，会出现顶栏已完整、抽屉还开着。
 */
const DRAWER_MEDIA_QUERY = '(max-width: 1023px)'

interface NavDrawerProps {
  id: string
  open: boolean
  onClose: () => void
  /** ☰ 按钮：关闭后焦点还给它 */
  menuButton: RefObject<HTMLButtonElement | null>
}

/**
 * <1024px 的左侧抽屉（DESIGN.md「桌面顶栏 · <1024px」）。原生模态 <dialog>：焦点移进去、页面其余部分不可交互；
 * Esc / ✕ / 点暗处 / 点抽屉里的链接都会关闭并把焦点还给 ☰。
 * 因浏览器前进后退而关闭时不把焦点拉回 ☰ —— 读者已经离开了那一页。
 */
export function NavDrawer({ id, open, onClose, menuButton }: NavDrawerProps) {
  const dialog = useRef<HTMLDialogElement>(null)
  // 整个地址（含查询与 hash）：在 /?q=a 与 /?q=b 之间前进后退也算离开了这一页
  const href = useLocation({ select: (location) => location.href })
  // 这一次关闭要不要把焦点还给 ☰（前进后退导致的关闭不还）
  const restoreFocus = useRef(true)
  const downOnBackdrop = useRef(false)

  useEffect(() => {
    const element = dialog.current
    if (!element) return
    if (open && !element.open) {
      restoreFocus.current = true
      element.showModal()
    } else if (!open && element.open) {
      element.close()
    }
  }, [open])

  // 窗口拉宽到完整顶栏时抽屉没有意义了：关掉；☰ 此时已隐藏，不还焦点
  useEffect(() => {
    if (!open || typeof window.matchMedia !== 'function') return
    const narrow = window.matchMedia(DRAWER_MEDIA_QUERY)
    const onChange = () => {
      if (narrow.matches) return
      restoreFocus.current = false
      dialog.current?.close()
    }
    narrow.addEventListener('change', onChange)
    return () => narrow.removeEventListener('change', onChange)
  }, [open])

  // 地址变了而抽屉还开着：只可能是前进 / 后退（点抽屉里的链接会先关抽屉）
  useEffect(() => {
    const element = dialog.current
    if (!element?.open) return
    restoreFocus.current = false
    element.close()
  }, [href])

  function handleClose() {
    onClose()
    const button = menuButton.current
    if (restoreFocus.current) {
      button?.focus({ preventScroll: true })
    } else if (document.activeElement === button) {
      // 浏览器关闭模态框时会自己把焦点还给打开它之前的元素；这一次不要
      button?.blur()
    }
    restoreFocus.current = true
  }

  function handleNavClick(event: MouseEvent<HTMLElement>) {
    if (event.target instanceof Element && event.target.closest('a')) dialog.current?.close()
  }

  return (
    <dialog
      ref={dialog}
      id={id}
      className="nav-drawer"
      aria-label="导航菜单"
      onClose={handleClose}
      onPointerDown={(event) => {
        downOnBackdrop.current = dialog.current !== null && isBackdropEvent(dialog.current, event)
      }}
      onClick={(event) => {
        // 按下和松开都在暗处才算点暗处：在抽屉里按下、拖到暗处松手不关
        if (downOnBackdrop.current && dialog.current && isBackdropEvent(dialog.current, event)) dialog.current.close()
        downOnBackdrop.current = false
      }}
    >
      <div className="nav-drawer-head">
        <Link to="/" className="app-wordmark" aria-label="nagare（发现）" onClick={() => dialog.current?.close()}>nagare</Link>
        <button type="button" className="app-header-icon" aria-label="关闭导航" onClick={() => dialog.current?.close()}>
          <Icon name="close" size={20} />
        </button>
      </div>
      <nav className="nav-drawer-nav" aria-label="站点导航" onClick={handleNavClick}>
        {PRIMARY_NAV.map(({ to, label, icon }) => (
          <Link key={to} to={to} className="nav-drawer-link" activeOptions={navActiveOptions(to)}>
            <Icon name={icon} size={20} />{label}
          </Link>
        ))}
        <hr className="nav-drawer-rule" />
        <Link to="/torrents" className="nav-drawer-link"><Icon name="download" size={20} />磁力任务</Link>
        <Link to="/settings" className="nav-drawer-link"><Icon name="settings" size={20} />设置</Link>
      </nav>
    </dialog>
  )
}
