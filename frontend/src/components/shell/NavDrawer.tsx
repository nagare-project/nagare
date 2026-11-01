import { useEffect, useRef } from 'react'
import type { MouseEvent, RefObject } from 'react'
import { Link, useLocation } from '@tanstack/react-router'
import { Icon } from '../ui/Icon'
import { isBackdropEvent } from '../../lib/dialog'
import { PRIMARY_NAV, navActiveOptions } from './navItems'

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
  const pathname = useLocation({ select: (location) => location.pathname })
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

  // 地址变了而抽屉还开着：只可能是前进 / 后退（点抽屉里的链接会先关抽屉）
  useEffect(() => {
    const element = dialog.current
    if (!element?.open) return
    restoreFocus.current = false
    element.close()
  }, [pathname])

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
      <nav className="nav-drawer-nav" aria-label="导航菜单" onClick={handleNavClick}>
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
