import { useCallback, useEffect, useState } from 'react'
import { ApiAuthError } from '../lib/api'
import { fetchSettings } from '../lib/endpoints'
import type { SettingsData } from '../lib/endpoints'
import { errorText } from '../lib/format'

/** 设置数据加载状态机（GET /api/settings） */
export type SettingsState =
  | { phase: 'loading' }
  | { phase: 'unauthorized' }
  | { phase: 'error'; message: string }
  | { phase: 'ready'; data: SettingsData }

export interface UseSettingsResult {
  state: SettingsState
  /** 重新拉取（登录/登出等改变设置的动作之后调用） */
  reload: () => Promise<void>
}

/**
 * 设置数据 hook。库页只用它点亮 mpv 状态点，设置页用全量。
 * 与 useLibrary 同构：ready 后 reload 不闪 loading。
 */
export function useSettings(): UseSettingsResult {
  const [state, setState] = useState<SettingsState>({ phase: 'loading' })

  const reload = useCallback(async () => {
    try {
      const data = await fetchSettings()
      setState({ phase: 'ready', data })
    } catch (err) {
      if (err instanceof ApiAuthError) {
        setState({ phase: 'unauthorized' })
        return
      }
      console.error('读取设置失败', err)
      setState({ phase: 'error', message: errorText(err, '读取设置失败') })
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  return { state, reload }
}
