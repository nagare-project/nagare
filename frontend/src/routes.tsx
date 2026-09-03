import { createRootRoute, createRoute, createRouter } from '@tanstack/react-router'
import type { RouterHistory } from '@tanstack/react-router'
import { RootLayout } from './components/RootLayout'
import { AnimePage } from './pages/AnimePage'
import { AutoDownloaderPage } from './pages/AutoDownloaderPage'
import { DiscoverPage } from './pages/DiscoverPage'
import { ListsPage } from './pages/ListsPage'
import { ScanSummariesPage } from './pages/ScanSummariesPage'
import { SchedulePage } from './pages/SchedulePage'
import { TorrentsPage } from './pages/TorrentsPage'
import { LibraryPage } from './pages/LibraryPage'
import { SearchPage } from './pages/SearchPage'
import { SettingsPage } from './pages/SettingsPage'

/** 根路由：新版本提示条 + Outlet（M4）；页面各自带完整壳（顶栏/页头） */
const rootRoute = createRootRoute({
  component: RootLayout,
})

/** `/` 媒体库主页（M1 起取代 M0 的连接检查壳） */
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: LibraryPage,
})

/**
 * `/anime/$clusterKey` 单部作品页：媒体库改成海报网格之后，剧集列表的落脚处。
 * clusterKey 会随重新扫描变化（文件增删导致重新归簇），所以页面必须处理
 * 「找不到这个 key」—— 那是正常的失效，不是错误。
 */
const animeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/anime/$clusterKey',
  component: AnimePage,
})

/**
 * 元数据三页（对齐 seanime）。它们现在吃 lib/fixtures 的假数据 ——
 * 后端缺口逐条记在仓库根的 todos.md，接通后删 fixture、改这三个组件的数据源。
 */
const listsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/lists', component: ListsPage })
const discoverRoute = createRoute({ getParentRoute: () => rootRoute, path: '/discover', component: DiscoverPage })
const scheduleRoute = createRoute({ getParentRoute: () => rootRoute, path: '/schedule', component: SchedulePage })

/** `/torrents` 磁力任务：走真实的 /api/torrent/status，没有假数据 */
const torrentsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/torrents', component: TorrentsPage })
const scanRoute = createRoute({ getParentRoute: () => rootRoute, path: '/scan-summaries', component: ScanSummariesPage })
const autoDlRoute = createRoute({ getParentRoute: () => rootRoute, path: '/auto-downloader', component: AutoDownloaderPage })

/** `/search` 的 query 形状：q 缺省或空白时省略，地址栏保持干净 */
export interface SearchRouteParams {
  q?: string
}

/**
 * 默认的 search 序列化会把 `?q=123` 解析成数字 123，这里统一收回字符串；
 * 非字符串 / 非数值的垃圾值与空白一律视作「未搜索」。
 */
function coerceQuery(raw: unknown): string | undefined {
  const text =
    typeof raw === 'string'
      ? raw
      : typeof raw === 'number' || typeof raw === 'boolean'
        ? String(raw)
        : ''
  const trimmed = text.trim()
  return trimmed === '' ? undefined : trimmed
}

/** `/search?q=` 磁力搜索页（M2）：关键词同步进 URL，刷新 / 分享可复现 */
const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/search',
  validateSearch: (search: Record<string, unknown>): SearchRouteParams => ({
    q: coerceQuery(search.q),
  }),
  component: SearchPage,
})

/** `/settings` 设置页：animego 账号 · mpv · 更新 · 磁力源 · 文件夹管理 · 关于 · 退出 */
const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: SettingsPage,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  animeRoute,
  listsRoute,
  discoverRoute,
  scheduleRoute,
  torrentsRoute,
  scanRoute,
  autoDlRoute,
  searchRoute,
  settingsRoute,
])

/**
 * 路由工厂：应用用默认的 browser history；测试可注入 memory history，
 * 每个用例一个干净的路由实例，互不串台。
 */
export function createAppRouter(history?: RouterHistory) {
  return createRouter(history === undefined ? { routeTree } : { routeTree, history })
}

export const router = createAppRouter()

// 类型注册：让 Link / useNavigate 等在整个 app 里拿到精确的路由类型推断
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
