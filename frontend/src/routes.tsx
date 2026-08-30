import { Outlet, createRootRoute, createRoute, createRouter } from '@tanstack/react-router'
import { LibraryPage } from './pages/LibraryPage'
import { SettingsPage } from './pages/SettingsPage'

/** 根路由：一层 Outlet；页面各自带完整壳（顶栏/页头），暂不需要全局布局 */
const rootRoute = createRootRoute({
  component: () => <Outlet />,
})

/** `/` 媒体库主页（M1 起取代 M0 的连接检查壳） */
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: LibraryPage,
})

/** `/settings` 设置页：animego 账号 · mpv 信息 · 文件夹管理 */
const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: SettingsPage,
})

const routeTree = rootRoute.addChildren([indexRoute, settingsRoute])

export const router = createRouter({ routeTree })

// 类型注册：让 Link / useNavigate 等在整个 app 里拿到精确的路由类型推断
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
