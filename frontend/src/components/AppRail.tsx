import { Link } from '@tanstack/react-router'

/**
 * 左侧图标导航栏 —— 全站唯一的导航入口。
 *
 * 改这个之前先知道为什么是这个形状：
 * 之前三个页面各自在顶栏里重复渲染「品牌 + 去别的页面的链接」，改一处要动三处，
 * 而且「当前在哪一页」没有任何视觉表达（当前页的链接和别的长得一样）。
 * 收成侧栏之后，导航只有一份，`aria-current` 由路由自动给出。
 *
 * 窄屏下 CSS 把它翻成顶部横条（见 global.css 的 max-width: 640px 段），
 * 结构不变 —— 不为窄屏另写一套 DOM，那会让两套导航的可达性各错一半。
 */

interface RailItem {
  to: '/' | '/lists' | '/discover' | '/schedule' | '/search' | '/settings'
  glyph: string
  label: string
}

/** 图标用字符不用 SVG：入口不多，字符在 4.5rem 宽下也够清楚，还省一套图标库 */
const ITEMS: readonly RailItem[] = [
  { to: '/', glyph: '▤', label: '媒体库' },
  { to: '/lists', glyph: '☰', label: '我的' },
  { to: '/discover', glyph: '◎', label: '发现' },
  { to: '/schedule', glyph: '▦', label: '放送' },
  { to: '/search', glyph: '⌕', label: '搜索' },
  { to: '/settings', glyph: '⚙', label: '设置' },
]

export function AppRail() {
  return (
    <nav className="app-rail" aria-label="主导航">
      <span className="app-rail-mark" aria-hidden="true">
        流
      </span>
      {ITEMS.map((item) => (
        <Link key={item.to} to={item.to} className="app-rail-item">
          <span className="app-rail-glyph" aria-hidden="true">
            {item.glyph}
          </span>
          {item.label}
        </Link>
      ))}
    </nav>
  )
}
