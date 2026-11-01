import { useId, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Icon } from '../ui/Icon'
import { useTorrentPlayback } from '../torrent/TorrentPlayContext'
import { HeaderSearch } from './HeaderSearch'
import { NavDrawer } from './NavDrawer'
import { PRIMARY_NAV, navActiveOptions } from './navItems'
import { useHeaderScroll } from './useHeaderScroll'
import '../../styles/navigation.css'

/**
 * 应用顶栏（DESIGN.md「桌面顶栏」）。
 * ≥1024px：字标 · 主导航 · 搜索 / 磁力任务（有任务时才出现）/ 设置；
 * <1024px：☰ · 字标 · 搜索，其余收进左侧抽屉。
 */
export function AppHeader() {
  const header = useRef<HTMLElement>(null)
  const menuButton = useRef<HTMLButtonElement>(null)
  const drawerId = useId()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const { scrolled, hidden, instant } = useHeaderScroll(header, drawerOpen || searchOpen)
  const torrent = useTorrentPlayback()
  const torrentActive = torrent.state.phase === 'starting' || torrent.state.phase === 'selecting' || torrent.state.phase === 'streaming'

  return (
    <>
      <header
        ref={header}
        className="app-header"
        data-scrolled={scrolled ? '' : undefined}
        data-hidden={hidden ? '' : undefined}
        data-instant={instant ? '' : undefined}
        data-search={searchOpen ? '' : undefined}
      >
        <div className="app-header-inner">
          <button ref={menuButton} type="button" className="app-header-icon app-header-menu" aria-label="打开导航"
            aria-expanded={drawerOpen} aria-controls={drawerId} onClick={() => setDrawerOpen(true)}>
            <Icon name="menu" size={20} />
          </button>
          <Link to="/" className="app-wordmark" aria-label="nagare（发现）">nagare</Link>
          <nav className="app-header-nav" aria-label="主导航">
            {PRIMARY_NAV.map(({ to, label }) => (
              <Link key={to} to={to} className="app-header-link" activeOptions={navActiveOptions(to)}>{label}</Link>
            ))}
          </nav>
          <div className="app-header-actions">
            <HeaderSearch open={searchOpen} onOpenChange={setSearchOpen} />
            {torrentActive && (
              <Link to="/torrents" className="app-header-icon app-header-torrent" aria-label="磁力任务（正在边下边播）" title="磁力任务">
                <Icon name="download" size={20} />
                <span className="app-header-torrent-dot" aria-hidden="true" />
              </Link>
            )}
            <Link to="/settings" className="app-header-icon app-header-settings" aria-label="设置" title="设置">
              <Icon name="settings" size={20} />
            </Link>
          </div>
        </div>
      </header>
      <NavDrawer id={drawerId} open={drawerOpen} onClose={() => setDrawerOpen(false)} menuButton={menuButton} />
    </>
  )
}
