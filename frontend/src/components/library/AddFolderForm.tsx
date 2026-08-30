import { useState } from 'react'
import type { FormEvent } from 'react'
import type { AddFolderData } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { mono } from '../../tokens'
import './library.css'

/** macOS 惯例的示例路径（M1 验收平台），提示「要绝对路径」 */
const PATH_PLACEHOLDER = '/Users/you/Movies/Anime'

export interface AddFolderFormProps {
  /** 提交回调（useLibrary.addFolder）；失败抛信封中文错误，由本组件就地展示 */
  onAdd: (path: string) => Promise<AddFolderData>
  autoFocus?: boolean
}

/** 表单结果行状态机 */
type SubmitState =
  | { phase: 'idle' }
  | { phase: 'busy' }
  | { phase: 'ok'; videos: number; clusters: number }
  | { phase: 'error'; message: string }

/**
 * 添加库文件夹：绝对路径文本输入 + 提交。
 * 成功展示本次扫描统计并清空输入；失败透出后端信封里的中文错误。
 */
export function AddFolderForm({ onAdd, autoFocus = false }: AddFolderFormProps) {
  const [path, setPath] = useState('')
  const [state, setState] = useState<SubmitState>({ phase: 'idle' })

  async function handleSubmit(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault()
    const trimmed = path.trim()
    if (trimmed === '') {
      setState({ phase: 'error', message: '请输入文件夹的绝对路径' })
      return
    }
    setState({ phase: 'busy' })
    try {
      const result = await onAdd(trimmed)
      setState({ phase: 'ok', videos: result.stats.videos, clusters: result.stats.clusters })
      setPath('')
    } catch (err) {
      // 错误不静默：控制台留全量上下文，界面透出信封中文文案
      console.error('添加文件夹失败', err)
      setState({ phase: 'error', message: errorText(err, '添加文件夹失败') })
    }
  }

  const view = resultView(state)

  return (
    <form className="add-folder" onSubmit={(event) => void handleSubmit(event)}>
      <div className="add-folder-row">
        <input
          className="hud-input"
          type="text"
          name="path"
          value={path}
          onChange={(event) => setPath(event.target.value)}
          placeholder={PATH_PLACEHOLDER}
          aria-label="文件夹绝对路径"
          autoFocus={autoFocus}
          spellCheck={false}
          autoComplete="off"
          disabled={state.phase === 'busy'}
        />
        <button
          type="submit"
          className="hud-button hud-button--small"
          disabled={state.phase === 'busy'}
        >
          {state.phase === 'busy' ? '扫描中 …' : '添加'}
        </button>
      </div>
      {/* 结果行常驻占位，出现/消失不推挤布局 */}
      <p
        className={view === null ? 'result' : `result result--${view.tone}`}
        style={mono}
        role="status"
        aria-live="polite"
      >
        {view?.text}
      </p>
    </form>
  )
}

function resultView(state: SubmitState): { tone: 'dim' | 'ok' | 'err'; text: string } | null {
  switch (state.phase) {
    case 'idle':
      return null
    case 'busy':
      return { tone: 'dim', text: '正在扫描该文件夹 …' }
    case 'ok':
      return { tone: 'ok', text: `已添加 · ${state.videos} 个视频 / ${state.clusters} 部作品` }
    case 'error':
      return { tone: 'err', text: state.message }
  }
}
