import { Link, useLocation } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import { Icon } from './ui/Icon'
import type { IconName } from './ui/Icon'
import '../styles/navigation.css'

const ITEMS = [
  { to: '/', icon: 'home', label: '媒体库' },
  { to: '/schedule', icon: 'calendar', label: '放送表' },
  { to: '/lists', icon: 'lists', label: '我的列表' },
  { to: '/discover', icon: 'compass', label: '发现' },
  { to: '/search', icon: 'search', label: '搜索' },
  { to: '/torrents', icon: 'download', label: '磁力任务' },
  { to: '/auto-downloader', icon: 'rss', label: '自动下载' },
  { to: '/debrid', icon: 'server', label: 'Debrid' },
] as const

/** 桌面端 80px 图标栏；窄屏用原生 dialog 抽屉提供焦点圈定与 Escape 关闭。 */
export function AppRail() {
  const pathname = useLocation({ select: (s) => s.pathname })
  const drawer = useRef<HTMLDialogElement>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => { if (drawer.current?.open) drawer.current.close(); setOpen(false) }, [pathname])

  function toggleDrawer() {
    if (drawer.current?.open) drawer.current.close()
    else drawer.current?.showModal()
    setOpen(drawer.current?.open ?? false)
  }

  function navigation(mobile: boolean) {
    return <>
      <Link to="/" className="app-rail-brand" aria-label="nagare 媒体库">
        <span className="app-rail-mark" aria-hidden="true">流</span>
        {mobile && <span className="app-brand-name">nagare</span>}
      </Link>
      <div className="app-rail-links">
        {ITEMS.map((item) => <RailLink key={item.to} {...item} mobile={mobile} />)}
      </div>
      <div className="app-rail-footer">
        <RailLink to="/extensions" icon="extension" label="扩展" mobile={mobile} />
        <RailLink to="/settings" icon="settings" label="设置" mobile={mobile} />
        <Link to="/settings" hash="account" className="app-rail-account" aria-label="账号设置" title="账号设置">
          <Icon name="user" size={20} />{mobile && <span>账号</span>}
        </Link>
      </div>
    </>
  }

  return <>
    <nav className="app-rail" aria-label="主导航">{navigation(false)}</nav>
    <header className="app-topbar">
      <button type="button" className="icon-button app-menu-button" aria-label="打开导航"
        aria-expanded={open} onClick={toggleDrawer}><Icon name="menu" /></button>
      <nav className="app-top-links" aria-label="页面导航">
        {ITEMS.slice(0, 4).map(({ to, label }) => <Link key={to} to={to} activeOptions={{ exact: true }}>{label}</Link>)}
      </nav>
      <Link to="/search" className="icon-button app-top-search" aria-label="搜索资源"><Icon name="search" size={20} /></Link>
    </header>
    <dialog className="app-drawer" ref={drawer} aria-label="导航菜单" onClose={() => setOpen(false)}
      onClick={(e) => { if (e.target === drawer.current) { drawer.current.close(); setOpen(false) } }}>
      <nav className="app-drawer-content" aria-label="移动导航" onClick={(event) => {
        if ((event.target as Element).closest('a')) { drawer.current?.close(); setOpen(false) }
      }}>
        <button type="button" className="icon-button app-drawer-close" onClick={toggleDrawer} aria-label="关闭导航"><Icon name="close" /></button>
        {navigation(true)}
      </nav>
    </dialog>
  </>
}

function RailLink({ to, icon, label, mobile }: { to: typeof ITEMS[number]['to'] | '/settings' | '/extensions'; icon: IconName; label: string; mobile: boolean }) {
  return <Link to={to} className="app-rail-item" activeOptions={{ exact: to === '/' }} aria-label={label}>
    <Icon name={icon} />
    <span className={mobile ? 'app-rail-label' : 'app-rail-tooltip'}>{label}</span>
  </Link>
}
