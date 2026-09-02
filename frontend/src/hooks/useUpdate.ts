import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { ApiAuthError } from '../lib/api'
import { checkUpdate, fetchUpdate, setUpdateEnabled } from '../lib/endpoints'
import type { UpdateView } from '../lib/endpoints'
import { errorText } from '../lib/format'

/** 更新状态加载状态机（GET /api/update） */
export type UpdateState =
  | { phase: 'loading' }
  | { phase: 'unauthorized' }
  | { phase: 'error'; message: string }
  | { phase: 'ready'; data: UpdateView }

export interface UseUpdateResult {
  state: UpdateState
  /** 重新拉取；已 ready 时保留旧数据直到新数据到达（不闪 loading） */
  reload: () => Promise<void>
  /**
   * 立即向 GitHub 查一次。查询本身失败不抛（落在返回值 error 字段里，状态照常 ready）；
   * 请求打不到后端才抛，由调用方就地展示。
   */
  check: () => Promise<UpdateView>
  /** 开关自动检查；成功后状态机同步为返回值，失败原样抛出 */
  setEnabled: (enabled: boolean) => Promise<UpdateView>
}

/**
 * 更新状态 hook。根布局加载一次，经 UpdateContext 共享给顶部提示条与设置页的更新卡 ——
 * 两处看到的是同一份数据，在设置页点「立即检查」后提示条立刻跟着变。
 * 与 useSettings 同构：只读失败进状态机；动作失败抛给调用方。
 */
export function useUpdate(): UseUpdateResult {
  const [state, setState] = useState<UpdateState>({ phase: 'loading' })

  const reload = useCallback(async () => {
    try {
      const data = await fetchUpdate()
      setState({ phase: 'ready', data })
    } catch (err) {
      if (err instanceof ApiAuthError) {
        setState({ phase: 'unauthorized' })
        return
      }
      console.error('读取更新状态失败', err)
      setState({ phase: 'error', message: errorText(err, '读取更新状态失败') })
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  const check = useCallback(async () => {
    const data = await checkUpdate()
    setState({ phase: 'ready', data })
    return data
  }, [])

  const setEnabled = useCallback(async (enabled: boolean) => {
    const data = await setUpdateEnabled(enabled)
    setState({ phase: 'ready', data })
    return data
  }, [])

  return { state, reload, check, setEnabled }
}

/** 由根布局提供；null 表示没套 Provider（属于装配错误，直接报） */
export const UpdateContext = createContext<UseUpdateResult | null>(null)

export function useUpdateContext(): UseUpdateResult {
  const value = useContext(UpdateContext)
  if (value === null) {
    throw new Error('useUpdateContext 必须在 UpdateContext.Provider 之内使用（见 RootLayout）')
  }
  return value
}
