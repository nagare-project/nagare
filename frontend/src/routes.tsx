import { Outlet, createRootRoute, createRoute, createRouter } from '@tanstack/react-router'
import { HomePage } from './pages/HomePage'

/** 根路由：M0 只有一层 Outlet，后续里程碑在这里挂全局布局与导航 */
const rootRoute = createRootRoute({
  component: () => <Outlet />,
})

/** 首页路由 */
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: HomePage,
})

const routeTree = rootRoute.addChildren([indexRoute])

export const router = createRouter({ routeTree })

// 类型注册：让 Link / useNavigate 等在整个 app 里拿到精确的路由类型推断
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
