import { createRootRoute, createRoute, createRouter, lazyRouteComponent, redirect } from '@tanstack/react-router'
import type { RouterHistory } from '@tanstack/react-router'
import { RootLayout } from './components/RootLayout'
import { SEASONAL_SORTS } from './components/catalog/season'
import { AnimePage } from './pages/AnimePage'
import { AutoDownloaderPage } from './pages/AutoDownloaderPage'
import { DebridPage } from './pages/DebridPage'
import { ExtensionsPage } from './pages/ExtensionsPage'
import { ListsPage } from './pages/ListsPage'
import { SchedulePage } from './pages/SchedulePage'
import { TorrentsPage } from './pages/TorrentsPage'
import { WatchPage } from './pages/WatchPage'
import { LibraryPage } from './pages/LibraryPage'
import { SearchPage } from './pages/SearchPage'
import { SettingsPage } from './pages/SettingsPage'

/** 根路由：新版本提示条 + Outlet（M4）；页面各自带完整壳（顶栏/页头） */
const rootRoute = createRootRoute({
  component: RootLayout,
})

/** `/discover?q=` 时显示作品搜索结果；genre 是热门板块的初始类型筛选（从作品页的类型标签跳过来） */
export interface DiscoverRouteParams {
  q?: string
  genre?: string
}

/** 认不出来的键显式写成 undefined（原因见 validateSeasonalSearch）：?q=[1]、?genre=1 不能原样漏给页面 */
function validateDiscoverSearch(search: Record<string, unknown>): DiscoverRouteParams {
  return {
    q: coerceQuery(search.q),
    genre: typeof search.genre === 'string' && search.genre !== '' ? search.genre : undefined,
  }
}

/** `/` 第一屏是发现页（DESIGN.md「信息架构」）：顶栏的搜索也落在这里（?q=） */
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  validateSearch: validateDiscoverSearch,
  component: lazyRouteComponent(() => import('./pages/DiscoverPage'), 'DiscoverPage'),
})

/**
 * `/discover` 是 `/` 的别名：旧书签、作品页类型标签（`/discover?genre=`）照样能用。
 * 参数校验与 `/` 共用一份，再原样带到 `/`（replace，后退不会回到这个跳板）。
 */
const discoverRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/discover',
  validateSearch: validateDiscoverSearch,
  beforeLoad: ({ search }) => {
    throw redirect({ to: '/', search, replace: true })
  },
})

/** `/library` 媒体库（原先在 `/`，第一屏让给发现页后挪到这里） */
const libraryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/library',
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

/** 元数据目录与账号收藏。 */
const listsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/lists', component: ListsPage })
const entryRoute = createRoute({ getParentRoute: () => rootRoute, path: '/entry', validateSearch: (search: Record<string, unknown>) => ({ id: Number.isSafeInteger(Number(search.id)) && Number(search.id) > 0 ? Number(search.id) : 0 }), component: lazyRouteComponent(() => import('./pages/EntryPage'), 'EntryPage') })
const scheduleRoute = createRoute({ getParentRoute: () => rootRoute, path: '/schedule', component: SchedulePage })


/**
 * 季度页的地址参数。认不出来的键【显式】写成 undefined 而不是省掉：根路由不校验参数，
 * TanStack 把这里的结果叠在地址栏原样的参数上，省掉的键会把坏值（?season=foo）原样漏给页面。
 */
function validateSeasonalSearch(search: Record<string, unknown>): Record<string, unknown> {
  const season = typeof search.season === 'string' ? search.season.toUpperCase() : ''
  const year = Number(search.year)
  const text = (value: unknown) => (typeof value === 'string' && value !== '' ? value : undefined)
  return {
    season: ['WINTER', 'SPRING', 'SUMMER', 'FALL'].includes(season) ? season : undefined,
    year: Number.isSafeInteger(year) && year >= 1990 && year <= 2100 ? year : undefined,
    genre: text(search.genre),
    format: text(search.format),
    status: text(search.status),
    sort: (SEASONAL_SORTS as readonly string[]).includes(text(search.sort) ?? '') ? search.sort : undefined,
  }
}

/** `/seasonal` 季度浏览：按年份 + 季度列目录；筛选与排序都在地址栏，可分享可回退。 */
const seasonalRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/seasonal',
  validateSearch: validateSeasonalSearch,
  component: lazyRouteComponent(() => import('./pages/SeasonalPage'), 'SeasonalPage'),
})

/** `/torrents` 磁力任务：走真实的 /api/torrent/status，没有假数据 */
const torrentsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/torrents', component: TorrentsPage })
const autoDlRoute = createRoute({ getParentRoute: () => rootRoute, path: '/auto-downloader', component: AutoDownloaderPage })

/**
 * `/extensions` 扩展：nagare 的声明式规则系统提成一等页面（真数据）。
 * 入口在设置页的分类导航里（顶栏不再放它，DESIGN.md「桌面顶栏」）。
 */
const extensionsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/extensions', component: ExtensionsPage })

/** `/debrid`：界面预览，后端未对接（缺口 G7） */
const debridRoute = createRoute({ getParentRoute: () => rootRoute, path: '/debrid', component: DebridPage })

/**
 * `/watch/$fileId` 浏览器内播放。决议 A5 原本不做，用户 2026-09-03 要求接上。
 * 它是补充路径：不转码、没有弹幕与 ASS 字幕、进度不回写。正常路径仍是 mpv。
 */
const watchRoute = createRoute({ getParentRoute: () => rootRoute, path: '/watch/$fileId', component: WatchPage })

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

/**
 * `/settings` 设置页：账号 + 三组（媒体库与 NAS / 播放 / 关于），分区在 #hash 里。
 * 分区表见 pages/settings/sections.ts。
 */
const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: SettingsPage,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  libraryRoute,
  animeRoute,
  listsRoute,
  discoverRoute,
  entryRoute,
  scheduleRoute,
  seasonalRoute,
  torrentsRoute,
  autoDlRoute,
  extensionsRoute,
  debridRoute,
  watchRoute,
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
