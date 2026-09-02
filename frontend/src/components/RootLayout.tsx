import { Outlet } from '@tanstack/react-router'
import { UpdateContext, useUpdate } from '../hooks/useUpdate'
import { UpdateBanner } from './UpdateBanner'

/**
 * 根布局：新版本提示条 + 页面出口。
 * 更新状态在这里只加载一次，经 UpdateContext 共享给设置页的更新卡 ——
 * 三个页面共用同一条提示，不必各自复制逻辑。
 */
export function RootLayout() {
  const update = useUpdate()
  return (
    <UpdateContext.Provider value={update}>
      <UpdateBanner view={update.state.phase === 'ready' ? update.state.data : null} />
      <Outlet />
    </UpdateContext.Provider>
  )
}
