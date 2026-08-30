import { useCallback, useEffect, useState } from 'react'
import { ApiAuthError } from '../lib/api'
import {
  addLibraryFolder,
  fetchLibrary,
  removeLibraryFolder,
  rescanLibrary,
} from '../lib/endpoints'
import type { AddFolderData, LibraryData, ScanStats } from '../lib/endpoints'
import { errorText } from '../lib/format'

/** 媒体库加载状态机 */
export type LibraryState =
  | { phase: 'loading' }
  | { phase: 'unauthorized' }
  | { phase: 'error'; message: string }
  | { phase: 'ready'; data: LibraryData }

export interface UseLibraryResult {
  state: LibraryState
  /** 重新拉取列表；已 ready 时保留旧数据直到新数据到达（不闪 loading） */
  reload: () => Promise<void>
  /** 添加文件夹并刷新列表；失败原样抛出（信封中文错误），由表单就地展示 */
  addFolder: (path: string) => Promise<AddFolderData>
  /** 删除文件夹并刷新列表；失败原样抛出 */
  removeFolder: (id: string) => Promise<void>
  /** 触发全量重扫并刷新列表，返回扫描统计；失败原样抛出 */
  rescan: () => Promise<ScanStats>
}

/**
 * 媒体库数据 hook：GET /api/library 的状态机 + 三个变更动作。
 * 变更动作成功后自动 reload，让列表始终反映后端最新扫描结果；
 * 动作的失败不进状态机（页面主体还好好的），抛给调用方就地提示。
 */
export function useLibrary(): UseLibraryResult {
  const [state, setState] = useState<LibraryState>({ phase: 'loading' })

  const reload = useCallback(async () => {
    try {
      const data = await fetchLibrary()
      setState({ phase: 'ready', data })
    } catch (err) {
      if (err instanceof ApiAuthError) {
        setState({ phase: 'unauthorized' })
        return
      }
      console.error('读取媒体库失败', err)
      setState({ phase: 'error', message: errorText(err, '读取媒体库失败') })
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  const addFolder = useCallback(
    async (path: string) => {
      const result = await addLibraryFolder(path)
      await reload()
      return result
    },
    [reload],
  )

  const removeFolder = useCallback(
    async (id: string) => {
      await removeLibraryFolder(id)
      await reload()
    },
    [reload],
  )

  const rescan = useCallback(async () => {
    const { stats } = await rescanLibrary()
    await reload()
    return stats
  }, [reload])

  return { state, reload, addFolder, removeFolder, rescan }
}
