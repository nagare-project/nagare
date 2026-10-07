import { useEffect, useId, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import type { AddFolderData, Platform } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { mono } from '../../theme'
import { FolderBrowser } from './FolderBrowser'
import './library.css'

/** 各平台惯例的示例路径，提示「要绝对路径」；平台未知时按 macOS（M1 验收平台） */
const PATH_PLACEHOLDER: Record<Platform, string> = {
  darwin: '/Users/you/Movies/Anime',
  windows: 'D:\\Anime',
  linux: '/home/you/Videos/Anime',
}

export interface AddFolderFormProps {
  /** 提交回调（useLibrary.addFolder）；失败抛信封中文错误，由本组件就地展示 */
  onAdd: (path: string) => Promise<AddFolderData>
  autoFocus?: boolean
  /** 后端所在平台（决定示例路径的写法） */
  platform?: Platform
}

/** 表单结果行状态机 */
type SubmitState =
  | { phase: 'idle' }
  | { phase: 'busy' }
  | { phase: 'ok'; videos: number; clusters: number }
  | { phase: 'error'; message: string }

/**
 * 添加库文件夹：绝对路径文本输入 + 提交，或者点「浏览…」逐层选目录（不用手敲路径）。
 * 成功展示本次扫描统计并清空输入；失败透出后端信封里的中文错误。
 */
export function AddFolderForm({ onAdd, autoFocus = false, platform = 'darwin' }: AddFolderFormProps) {
  const [path, setPath] = useState('')
  const [state, setState] = useState<SubmitState>({ phase: 'idle' })
  const [browsing, setBrowsing] = useState(false)
  const browseRef = useRef<HTMLButtonElement>(null)
  const returnFocus = useRef(false)
  const panelId = useId()

  // 面板收起后把焦点还给「浏览…」。要等这次渲染完：添加进行中按钮是禁用的，禁用的按钮拿不到焦点
  useEffect(() => {
    if (browsing || state.phase === 'busy' || !returnFocus.current) return
    returnFocus.current = false
    browseRef.current?.focus()
  }, [browsing, state.phase])

  async function handleSubmit(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault()
    await submit(path)
  }

  /**
   * 在浏览面板里选定目录：路径回填进输入框（让人看见加的是哪个），随即添加。
   * 加成功才收起面板 —— 失败时用户还停在那一层，不必从「常用位置」重新点起。
   */
  async function handleChoose(chosen: string): Promise<void> {
    setPath(chosen)
    if (await submit(chosen)) closeBrowser()
  }

  /** 收起面板并把焦点还给「浏览…」，键盘用户不会被甩回页面顶部 */
  function closeBrowser(): void {
    returnFocus.current = true
    setBrowsing(false)
  }

  /** 返回是否添加成功 */
  async function submit(raw: string): Promise<boolean> {
    const trimmed = raw.trim()
    if (trimmed === '') {
      setState({ phase: 'error', message: '请输入文件夹的绝对路径，或点「浏览…」选择' })
      return false
    }
    setState({ phase: 'busy' })
    try {
      const result = await onAdd(trimmed)
      setState({ phase: 'ok', videos: result.stats.videos, clusters: result.stats.clusters })
      setPath('')
      return true
    } catch (err) {
      // 错误不静默：控制台留全量上下文，界面透出信封中文文案
      console.error('添加文件夹失败', err)
      setState({ phase: 'error', message: errorText(err, '添加文件夹失败') })
      return false
    }
  }

  const view = resultView(state)

  return (
    <form className="add-folder" onSubmit={(event) => void handleSubmit(event)}>
      <div className="add-folder-row">
        <input
          className="input"
          type="text"
          name="path"
          value={path}
          onChange={(event) => setPath(event.target.value)}
          placeholder={PATH_PLACEHOLDER[platform]}
          aria-label="文件夹绝对路径"
          autoFocus={autoFocus}
          spellCheck={false}
          autoComplete="off"
          disabled={state.phase === 'busy'}
        />
        <button
          ref={browseRef}
          type="button"
          className="btn btn--sm"
          onClick={() => (browsing ? closeBrowser() : setBrowsing(true))}
          aria-expanded={browsing}
          aria-controls={browsing ? panelId : undefined}
          disabled={state.phase === 'busy'}
        >
          浏览…
        </button>
        <button
          type="submit"
          className="btn btn--sm btn--primary"
          disabled={state.phase === 'busy'}
        >
          {state.phase === 'busy' ? '扫描中 …' : '添加'}
        </button>
      </div>
      {browsing && (
        <FolderBrowser
          id={panelId}
          onChoose={(chosen) => void handleChoose(chosen)}
          onCancel={closeBrowser}
          busy={state.phase === 'busy'}
        />
      )}
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
