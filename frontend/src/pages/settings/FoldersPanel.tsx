import { useEffect, useRef, useState } from 'react'
import { AddFolderForm } from '../../components/library/AddFolderForm'
import type { UseLibraryResult } from '../../hooks/useLibrary'
import type { Platform } from '../../lib/endpoints'
import { handOffFocus } from '../../lib/focus'
import { errorText, formatDate } from '../../lib/format'
import { mono } from '../../theme'

export interface FoldersPanelProps {
  library: UseLibraryResult
  platform?: Platform
}

/**
 * 媒体文件夹：已添加的列表（含扫描出错的原因）+ 删除，下面是添加表单。
 * NAS 没有单独的连接方式：在系统里连上共享（访达「连接服务器」、资源管理器「映射网络驱动器」），
 * 挂载出来的文件夹与外接硬盘一样按文件夹添加。
 */
export function FoldersPanel({ library, platform }: FoldersPanelProps) {
  const { state } = library
  const [removingId, setRemovingId] = useState<string | null>(null)
  const [removeError, setRemoveError] = useState<string | null>(null)
  const panel = useRef<HTMLElement>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  // 删掉的那一行连同「删除」按钮一起消失：焦点交给列表标题，不掉到 <body> 上
  const handOff = useRef(false)
  useEffect(() => {
    if (removingId !== null || !handOff.current) return
    handOff.current = false
    handOffFocus(heading.current, panel.current)
  }, [removingId])

  async function handleRemove(id: string): Promise<void> {
    setRemovingId(id)
    setRemoveError(null)
    try {
      await library.removeFolder(id)
      handOff.current = true
    } catch (err) {
      console.error('删除文件夹失败', err)
      setRemoveError(errorText(err, '删除文件夹失败'))
    } finally {
      setRemovingId(null)
    }
  }

  return (
    <>
      <section ref={panel} className="info-panel" aria-labelledby="folders-heading">
        <h3 ref={heading} id="folders-heading" className="info-panel-title" tabIndex={-1}>
          已添加的文件夹
        </h3>
        {state.phase === 'loading' && (
          <p className="result result--dim" role="status">
            正在读取文件夹列表 …
          </p>
        )}
        {state.phase === 'error' && (
          <div className="form-actions">
            <p className="result result--err" role="alert">
              {state.message}
            </p>
            <button type="button" className="btn btn--sm" onClick={() => void library.reload()}>
              重试
            </button>
          </div>
        )}
        {state.phase === 'ready' && state.data.folders.length === 0 && <p className="info-panel-text">还没有添加任何文件夹。</p>}
        {state.phase === 'ready' && state.data.folders.length > 0 && (
          <ul className="folder-list">
            {state.data.folders.map((folder) => (
              <li key={folder.id} className="folder-row">
                <span className="folder-path" style={mono} title={folder.path}>
                  <bdi>{folder.path}</bdi>
                </span>
                <span className="folder-date" style={mono}>
                  {formatDate(folder.addedAt)}
                </span>
                {/* 名字以看得见的字开头（语音控制按它找），后面补上是哪个文件夹 */}
                <button type="button" className="btn btn--sm btn--danger folder-remove" onClick={() => void handleRemove(folder.id)} disabled={removingId !== null}>
                  {removingId === folder.id ? '删除中 …' : '删除'}
                  <span className="visually-hidden">文件夹 {folder.path}</span>
                </button>
                {folder.error !== undefined && folder.error !== '' && (
                  <span className="folder-error" role="alert">
                    扫描出错：{folder.error}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
        {removeError !== null && (
          <p className="result result--err" role="alert">
            {removeError}
          </p>
        )}
      </section>

      {state.phase === 'ready' && (
        <section className="info-panel" aria-labelledby="add-folder-heading">
          <h3 id="add-folder-heading" className="info-panel-title">
            添加文件夹
          </h3>
          <p className="info-panel-text">输入绝对路径，或点「浏览…」逐层选择。NAS 先在系统里连接共享（访达「连接服务器」、资源管理器「映射网络驱动器」），再像外接硬盘一样添加挂载出来的文件夹。</p>
          <AddFolderForm onAdd={library.addFolder} platform={platform} />
        </section>
      )}
    </>
  )
}
