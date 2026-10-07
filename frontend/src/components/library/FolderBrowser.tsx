import { useEffect, useRef, useState } from 'react'
import { browseDirs } from '../../lib/endpoints'
import type { DirListing } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { mono } from '../../theme'
import { Icon } from '../ui/Icon'
import './folder-browser.css'

export interface FolderBrowserProps {
  /** 选定了一个目录（绝对路径） */
  onChoose: (path: string) => void
  onCancel: () => void
  /** 父表单正在提交：禁用「添加」 */
  busy?: boolean
}

type BrowseState =
  | { phase: 'loading'; path: string }
  | { phase: 'ready'; listing: DirListing }
  | { phase: 'error'; path: string; message: string }

/**
 * 逐层点选本机目录（照 seanime 的 directory-selector）。
 *
 * 新用户最难的一步不是「要不要加文件夹」，而是手敲 `/Users/xxx/Movies/Anime` 这种绝对路径 ——
 * 浏览器拿不到本地绝对路径，所以由后端列目录名（GET /api/fs/dirs，只有目录、没有文件）。
 * 起点是「常用位置」：主目录、影片、下载、桌面和挂着的外接硬盘。
 */
export function FolderBrowser({ onChoose, onCancel, busy = false }: FolderBrowserProps) {
  const [state, setState] = useState<BrowseState>({ phase: 'loading', path: '' })
  const inflight = useRef<AbortController | null>(null)

  function load(path: string): void {
    inflight.current?.abort()
    const abort = new AbortController()
    inflight.current = abort
    setState({ phase: 'loading', path })
    browseDirs(path, abort.signal)
      .then((listing) => {
        if (!abort.signal.aborted) setState({ phase: 'ready', listing })
      })
      .catch((err: unknown) => {
        if (abort.signal.aborted) return
        console.error('读取文件夹失败', err)
        setState({ phase: 'error', path, message: errorText(err, '读取文件夹失败') })
      })
  }

  useEffect(() => {
    load('')
    return () => inflight.current?.abort()
  }, [])

  const listing = state.phase === 'ready' ? state.listing : null
  const path = listing?.path ?? (state.phase === 'ready' ? '' : state.path)
  const atPlaces = path === ''

  return (
    <div className="folder-browser" role="group" aria-label="选择文件夹">
      <div className="folder-browser-head">
        <button type="button" className="btn btn--sm" onClick={() => load('')} disabled={atPlaces && state.phase !== 'error'}>
          常用位置
        </button>
        {listing !== null && listing.parent !== '' && (
          <button type="button" className="btn btn--sm" onClick={() => load(listing.parent)} aria-label="上一级文件夹">
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

      {state.phase === 'error' ? (
        <p className="result result--err folder-browser-message" role="alert">
          {state.message}
        </p>
      ) : (
        <ul className="folder-browser-list" aria-busy={state.phase === 'loading'}>
          {state.phase === 'loading' && <li className="folder-browser-message result result--dim">正在读取 …</li>}
          {listing !== null && listing.dirs.length === 0 && (
            <li className="folder-browser-message result result--dim">
              {atPlaces ? '没有找到常用位置，请直接在上面输入路径' : '这里没有子文件夹'}
            </li>
          )}
          {listing?.dirs.map((dir) => (
            <li key={dir.path}>
              <button type="button" className="folder-browser-item" onClick={() => load(dir.path)}>
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
      )}
      {listing?.truncated === true && <p className="result result--dim">子文件夹太多，只列出了前 1000 个。</p>}

      <div className="folder-browser-foot">
        <button
          type="button"
          className="btn btn--sm btn--primary"
          onClick={() => onChoose(path)}
          disabled={atPlaces || state.phase !== 'ready' || busy}
        >
          添加这个文件夹
        </button>
        <button type="button" className="btn btn--sm" onClick={onCancel}>
          取消
        </button>
        {atPlaces && <span className="result result--dim">先点进存放动漫的那个文件夹</span>}
      </div>
    </div>
  )
}
