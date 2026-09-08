import { useCallback, useEffect, useState } from 'react'
import { ApiAuthError } from '../lib/api'
import { fetchSourcePlugin, updateSourcePluginConfig } from '../lib/endpoints'
import type { SourcePluginConfig, SourcePluginView } from '../lib/endpoints'
import { errorText } from '../lib/format'

export type SourcePluginState =
  | { phase: 'loading' }
  | { phase: 'unauthorized' }
  | { phase: 'error'; message: string }
  | { phase: 'ready'; data: SourcePluginView }

export interface UseSourcePluginResult {
  state: SourcePluginState
  reload: () => Promise<boolean>
  save: (config: SourcePluginConfig) => Promise<SourcePluginView>
}

/** 本地来源插件的读取与配置状态机。 */
export function useSourcePlugin(): UseSourcePluginResult {
  const [state, setState] = useState<SourcePluginState>({ phase: 'loading' })

  const reload = useCallback(async (): Promise<boolean> => {
    try {
      const data = await fetchSourcePlugin()
      setState({ phase: 'ready', data })
      return true
    } catch (err) {
      if (err instanceof ApiAuthError) {
        setState({ phase: 'unauthorized' })
        return false
      }
      console.error('读取来源插件失败', err)
      setState({ phase: 'error', message: errorText(err, '读取来源插件失败') })
      return false
    }
  }, [])

  useEffect(() => { void reload() }, [reload])

  const save = useCallback(async (config: SourcePluginConfig) => {
    const data = await updateSourcePluginConfig(config)
    setState({ phase: 'ready', data })
    return data
  }, [])

  return { state, reload, save }
}
