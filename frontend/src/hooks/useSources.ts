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

/** 变更已落地但列表没刷成功时抛出的文案（调用方原样展示） */
export const REFRESH_FAILED_MESSAGE = '操作已生效，但刷新源列表失败，请点「重新加载」'

/** 磁力源（规则）数据加载状态机（GET /api/sources） */
export type SourcesState =
  | { phase: 'loading' }
  | { phase: 'unauthorized' }
  | { phase: 'error'; message: string }
  | { phase: 'ready'; data: SourcesData }

export interface UseSourcesResult {
  state: SourcesState
  /**
   * 重新拉取；已 ready 时保留旧数据直到新数据到达（不闪 loading）。
   * 返回是否成功——失败已进状态机（error/unauthorized），调用方据此决定要不要把
   * "操作已生效"说成"一切正常"。
   */
  reload: () => Promise<boolean>
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

  const reload = useCallback(async (): Promise<boolean> => {
    try {
      const data = await fetchSources()
      setState({ phase: 'ready', data })
      return true
    } catch (err) {
      if (err instanceof ApiAuthError) {
        setState({ phase: 'unauthorized' })
        return false
      }
      console.error('读取磁力源失败', err)
      setState({ phase: 'error', message: errorText(err, '读取磁力源失败') })
      return false
    }
  }, [])

  // 变更成功、但随后的列表刷新失败：不能让调用方以为一切正常——列表已经是过期/错误态。
  const reloadOrThrow = useCallback(async () => {
    if (!(await reload())) {
      throw new Error(REFRESH_FAILED_MESSAGE)
    }
  }, [reload])

  useEffect(() => {
    void reload()
  }, [reload])

  const setEnabled = useCallback(
    async (id: string, enabled: boolean) => {
      await setSourceEnabled(id, enabled)
      await reloadOrThrow()
    },
    [reloadOrThrow],
  )

  const selfCheck = useCallback((id: string) => selfCheckSource(id), [])

  const reloadRules = useCallback(async () => {
    const result = await reloadSources()
    await reloadOrThrow()
    return result
  }, [reloadOrThrow])

  const saveConfig = useCallback(
    async (patch: RulesConfigPatch) => {
      const rules = await updateRulesConfig(patch)
      await reloadOrThrow()
      return rules
    },
    [reloadOrThrow],
  )

  const sync = useCallback(async () => {
    const result = await syncSources()
    await reloadOrThrow()
    return result
  }, [reloadOrThrow])

  return { state, reload, setEnabled, selfCheck, reloadRules, saveConfig, sync }
}
