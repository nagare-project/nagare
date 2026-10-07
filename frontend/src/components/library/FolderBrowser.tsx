import { useEffect, useRef, useState } from 'react'
import type { KeyboardEvent } from 'react'
import { browseDirs } from '../../lib/endpoints'
import type { DirListing } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { mono } from '../../theme'
import { Icon } from '../ui/Icon'
import './folder-browser.css'

export interface FolderBrowserProps {
  /** 面板容器的 id（「浏览…」按钮的 aria-controls 指向它） */
  id?: string
  /** 选定了一个目录（绝对路径） */
  onChoose: (path: string) => void
  /** 取消或按 Esc：由父组件收起面板并把焦点还给「浏览…」 */
  onCancel: () => void
  /** 父表单正在提交：禁用「添加」 */
  busy?: boolean
}

/**
 * 逐层点选本机目录（照 seanime 的 directory-selector）。
 *
 * 新用户最难的一步不是「要不要加文件夹」，而是手敲 `/Users/xxx/Movies/Anime` 这种绝对路径 ——
 * 浏览器拿不到本地绝对路径，所以由后端列目录名（GET /api/fs/dirs，只有目录、没有文件）。
 * 起点是「常用位置」：主目录、影片、下载、桌面和挂着的外接硬盘。
 *
 * 键盘与读屏：读取下一层时旧列表留在原地（只是变暗），读完把焦点放到新列表的第一项，
 * 并在状态区念出当前位置 —— 换掉被聚焦的元素会把焦点甩回页面顶部，每点一下都得从头找。
 * 「添加」放在列表上方，不必先 Tab 过上千个文件夹；Esc 关闭。
 */
export function FolderBrowser({ id, onChoose, onCancel, busy = false }: FolderBrowserProps) {
  const [listing, setListing] = useState<DirListing | null>(null)
  const [loadingPath, setLoadingPath] = useState<string | null>('')
  const [error, setError] = useState<string | null>(null)
  const inflight = useRef<AbortController | null>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const addRef = useRef<HTMLButtonElement>(null)
  const focusAfterLoad = useRef(true)

  // 只在挂载时读一次「常用位置」；load 每次渲染都是新函数，但它不依赖任何会变的 props/state
  useEffect(() => {
    load('')
    return () => inflight.current?.abort()
  }, [])

  // 新的一层读完：把焦点放进列表（没有子文件夹时放到「添加」上）
  useEffect(() => {
    if (listing === null || !focusAfterLoad.current) return
    focusAfterLoad.current = false
    const first = listRef.current?.querySelector<HTMLButtonElement>('button')
    ;(first ?? addRef.current)?.focus()
  }, [listing])

  function load(path: string): void {
    inflight.current?.abort()
    const abort = new AbortController()
    inflight.current = abort
    setLoadingPath(path)
    setError(null)
    browseDirs(path, abort.signal)
      .then((next) => {
        if (abort.signal.aborted) return
        focusAfterLoad.current = true
        setListing(next)
        setLoadingPath(null)
      })
      .catch((err: unknown) => {
        if (abort.signal.aborted) return
        console.error('读取文件夹失败', err)
        setLoadingPath(null)
        setError(errorText(err, '读取文件夹失败'))
      })
  }

  /** 读取进行中不再响应点击：防双击连进两层 */
  function enter(path: string): void {
    if (loadingPath !== null) return
    load(path)
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>): void {
    if (event.key === 'Escape') {
      event.stopPropagation()
      onCancel()
    }
  }

  const loading = loadingPath !== null
  const path = listing?.path ?? ''
  const atPlaces = path === ''
  const announce = loading
    ? '正在读取 …'
    : error !== null
      ? ''
      : listing === null
        ? ''
        : `${atPlaces ? '常用位置' : path}，${listing.dirs.length} 个文件夹`

  return (
    <div id={id} className="folder-browser" role="group" aria-label="选择文件夹" onKeyDown={handleKeyDown}>
      <div className="folder-browser-head">
        <button type="button" className="btn btn--sm" onClick={() => enter('')} disabled={atPlaces && error === null}>
          常用位置
        </button>
        {listing !== null && listing.parent !== '' && (
          <button type="button" className="btn btn--sm" onClick={() => enter(listing.parent)} aria-label="上一级文件夹">
            <Icon name="left" size={14} />
            上一级
          </button>
        )}
        {!atPlaces && (
          <span className="folder-browser-path" style={mono} title={path}>
            <bdi>{path}</bdi>
          </span>
        )}
      </div>

      <div className="folder-browser-foot">
        <button
          ref={addRef}
          type="button"
          className="btn btn--sm btn--primary"
          onClick={() => onChoose(path)}
          disabled={atPlaces || loading || busy}
        >
          添加这个文件夹
        </button>
        <button type="button" className="btn btn--sm" onClick={onCancel}>
          取消
        </button>
        {atPlaces && !loading && <span className="result result--dim">先点进存放动漫的那个文件夹</span>}
      </div>

      <p className="visually-hidden" role="status" aria-live="polite">
        {announce}
      </p>
      {error !== null && (
        <p className="result result--err folder-browser-message" role="alert">
          {error}
        </p>
      )}

      <ul ref={listRef} role="list" className="folder-browser-list" aria-busy={loading} data-loading={loading || undefined}>
        {listing === null && loading && <li className="folder-browser-message result result--dim">正在读取 …</li>}
        {listing !== null && listing.dirs.length === 0 && (
          <li className="folder-browser-message result result--dim">
            {atPlaces ? '没有找到常用位置，请直接在上面输入路径' : '这里没有子文件夹'}
          </li>
        )}
        {listing?.dirs.map((dir) => (
          <li key={dir.path}>
            <button type="button" className="folder-browser-item" onClick={() => enter(dir.path)} title={dir.path}>
              <Icon name="folder" size={16} />
              <span className="folder-browser-name">{dir.name}</span>
              {atPlaces && (
                <span className="folder-browser-sub" style={mono}>
                  <bdi>{dir.path}</bdi>
                </span>
              )}
              <Icon name="right" size={14} />
            </button>
          </li>
        ))}
      </ul>
      {listing?.truncated === true && <p className="result result--dim">子文件夹太多，只列出了一部分。</p>}
    </div>
  )
}
