import { useCallback, useEffect, useState } from 'react'
import { ApiAuthError } from '../lib/api'
import {
  fetchSources,
  reloadSources,
  selfCheckSource,
  setSourceEnabled,
  syncSources,
  updateRulesConfig,
} from '../lib/endpoints'
import type {
  ReloadResult,
  RulesConfigPatch,
  RulesInfo,
  SourceOutcome,
  SourcesData,
  SyncResult,
} from '../lib/endpoints'
import { errorText } from '../lib/format'

/** 磁力源（规则）数据加载状态机（GET /api/sources） */
export type SourcesState =
  | { phase: 'loading' }
  | { phase: 'unauthorized' }
  | { phase: 'error'; message: string }
  | { phase: 'ready'; data: SourcesData }

export interface UseSourcesResult {
  state: SourcesState
  /** 重新拉取；已 ready 时保留旧数据直到新数据到达（不闪 loading） */
  reload: () => Promise<void>
  /** 启用 / 禁用一个源并刷新列表；失败原样抛出（信封中文错误） */
  setEnabled: (id: string, enabled: boolean) => Promise<void>
  /** 用规则自带关键词探活；不改变列表，结果交给调用方展示 */
  selfCheck: (id: string) => Promise<SourceOutcome>
  /** 重新加载规则目录并刷新列表 */
  reloadRules: () => Promise<ReloadResult>
  /** 保存规则来源配置并刷新列表 */
  saveConfig: (patch: RulesConfigPatch) => Promise<RulesInfo>
  /** 从 remoteUrl 拉取规则并刷新列表 */
  sync: () => Promise<SyncResult>
}

/**
 * 磁力源数据 hook：GET /api/sources 的状态机 + 变更动作。
 * 与 useLibrary 同构：变更成功后自动 reload；动作失败不进状态机，抛给调用方就地提示。
 */
export function useSources(): UseSourcesResult {
  const [state, setState] = useState<SourcesState>({ phase: 'loading' })

  const reload = useCallback(async () => {
    try {
      const data = await fetchSources()
      setState({ phase: 'ready', data })
    } catch (err) {
      if (err instanceof ApiAuthError) {
        setState({ phase: 'unauthorized' })
        return
      }
      console.error('读取磁力源失败', err)
      setState({ phase: 'error', message: errorText(err, '读取磁力源失败') })
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  const setEnabled = useCallback(
    async (id: string, enabled: boolean) => {
      await setSourceEnabled(id, enabled)
      await reload()
    },
    [reload],
  )

  const selfCheck = useCallback((id: string) => selfCheckSource(id), [])

  const reloadRules = useCallback(async () => {
    const result = await reloadSources()
    await reload()
    return result
  }, [reload])

  const saveConfig = useCallback(
    async (patch: RulesConfigPatch) => {
      const rules = await updateRulesConfig(patch)
      await reload()
      return rules
    },
    [reload],
  )

  const sync = useCallback(async () => {
    const result = await syncSources()
    await reload()
    return result
  }, [reload])

  return { state, reload, setEnabled, selfCheck, reloadRules, saveConfig, sync }
}
