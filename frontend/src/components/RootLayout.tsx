import { Outlet } from '@tanstack/react-router'
import { AppRail } from './AppRail'
import { SelfUpdateContext, useSelfUpdate } from '../hooks/useSelfUpdate'
import { UpdateContext, useUpdate } from '../hooks/useUpdate'
import { UpdateBanner } from './UpdateBanner'

/**
 * 根布局：侧边导航 + 新版本提示条 + 页面出口。
 * 更新状态在这里只加载一次，经 UpdateContext 共享给设置页的更新卡 ——
 * 三个页面共用同一条提示，不必各自复制逻辑。
 *
 * 一键更新流程同样挂在这里：它要跨几分钟（下载 + 校验 + 等重启），
 * 挂在设置页上的话用户一切页面就把整个流程卸载了。
 */
export function RootLayout() {
  const update = useUpdate()
  const selfUpdate = useSelfUpdate()
  return (
    <UpdateContext.Provider value={update}>
      <SelfUpdateContext.Provider value={selfUpdate}>
        <AppRail />
        <div className="app-main">
          <UpdateBanner
            view={update.state.phase === 'ready' ? update.state.data : null}
            selfUpdate={selfUpdate}
          />
          <Outlet />
        </div>
      </SelfUpdateContext.Provider>
    </UpdateContext.Provider>
  )
}
