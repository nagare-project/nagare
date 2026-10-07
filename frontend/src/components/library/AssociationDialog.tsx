import { useEffect, useId, useRef, useState } from 'react'
import type { MouseEvent, PointerEvent } from 'react'
import type { LibraryCluster } from '../../lib/endpoints'
import { errorText } from '../../lib/format'
import { fetchCatalogSearch } from '../../lib/media'
import { Icon } from '../ui/Icon'
import { FORMAT_LABELS } from '../media/media-format'
import type { MediaSummary } from '../media/types'
import './association.css'

/** 与后端 maxSearchRunes 一致：再长的关键词后端会拒绝 */
const MAX_QUERY_LENGTH = 64

type SearchState =
  | { phase: 'idle' }
  | { phase: 'loading'; query: string }
  | { phase: 'ready'; items: MediaSummary[] }
  | { phase: 'error'; message: string }

export type DialogChoice = { mode: 'manual'; anilistId: number } | { mode: 'none' }

interface Props {
  cluster: LibraryCluster
  open: boolean
  onClose: () => void
  /** 保存选择（由面板执行，失败时抛出）；成功后对话框自己关上 */
  onChoose: (choice: DialogChoice) => Promise<void>
  /** 对话框已经被关掉之后保存才失败：错误交给面板显示，不能悄悄丢掉 */
  onLateError: (message: string) => void
}

const clampQuery = (text: string) => [...text].slice(0, MAX_QUERY_LENGTH).join('')

/** 点在对话框框体之外（遮罩上）。框体内的滚动条也算 target === dialog，所以按坐标判断 */
function onBackdrop(dialog: HTMLDialogElement, event: MouseEvent | PointerEvent): boolean {
  if (event.target !== dialog) return false
  const r = dialog.getBoundingClientRect()
  return event.clientX < r.left || event.clientX > r.right || event.clientY < r.top || event.clientY > r.bottom
}

/**
 * 选择作品分组对应的目录作品。只在提交时搜：目录搜索有限速（后端另有缓存与节流兜底），
 * 边打字边搜会把配额烧光。打开时用分组标题先搜一次。
 *
 * 保存期间也允许关闭（Esc / 遮罩 / 关闭按钮）：保存可能要等上游一阵，不能把人关在模态框里；
 * 保存照常进行，结果由面板显示。
 */
export function AssociationDialog({ cluster, open, onClose, onChoose, onLateError }: Props) {
  const dialog = useRef<HTMLDialogElement>(null)
  const input = useRef<HTMLInputElement>(null)
  // 在途的那次搜索：用 ref 记，请求取消或结束就清掉 —— 不能看渲染出来的 loading 状态，
  // 关掉对话框会取消请求，状态却可能还停在 loading，重开后同一个词就再也搜不出去
  const inFlight = useRef<{ query: string; abort: AbortController } | null>(null)
  const saving = useRef(false)
  const downOnBackdrop = useRef(false)
  const headingId = useId()
  const [query, setQuery] = useState(() => clampQuery(cluster.title))
  const [results, setResults] = useState<SearchState>({ phase: 'idle' })
  const [pending, setPending] = useState<number | 'none' | null>(null)
  const [error, setError] = useState<string | null>(null)

  function cancelSearch(): void {
    inFlight.current?.abort.abort()
    inFlight.current = null
  }

  function runSearch(text: string): void {
    const q = clampQuery(text.trim())
    // 同一个词还在搜：不再打一次（搜索端点有限速）
    if (inFlight.current?.query === q) return
    cancelSearch()
    if (q === '') {
      setResults({ phase: 'idle' })
      return
    }
    const abort = new AbortController()
    const request = { query: q, abort }
    inFlight.current = request
    setResults({ phase: 'loading', query: q })
    fetchCatalogSearch(q, abort.signal)
      .then((items) => {
        if (!abort.signal.aborted) setResults({ phase: 'ready', items })
      })
      .catch((err: unknown) => {
        if (abort.signal.aborted) return
        console.error('搜索作品失败', err)
        setResults({ phase: 'error', message: errorText(err, '搜索失败') })
      })
      .finally(() => {
        if (inFlight.current === request) inFlight.current = null
      })
  }

  // 打开时重置并先搜一次。只跟着 open 变：重新渲染时不该把用户改过的关键词冲掉
  useEffect(() => {
    const element = dialog.current
    if (!element) return
    if (open && !element.open) {
      const initial = clampQuery(cluster.title)
      setQuery(initial)
      setError(null)
      element.showModal()
      // 有鼠标键盘时直接把焦点放进搜索框；触屏上先别弹键盘，让人先看到自动搜出来的结果
      if (window.matchMedia?.('(pointer: fine)').matches ?? true) input.current?.focus()
      runSearch(initial)
    } else if (!open && element.open) {
      element.close()
    }
  }, [open])

  useEffect(() => cancelSearch, [])

  async function choose(choice: DialogChoice): Promise<void> {
    if (saving.current) return
    saving.current = true
    setPending(choice.mode === 'manual' ? choice.anilistId : 'none')
    setError(null)
    try {
      await onChoose(choice)
      dialog.current?.close()
    } catch (err) {
      const message = errorText(err, '保存失败')
      if (dialog.current?.open) setError(message)
      else onLateError(message)
    } finally {
      saving.current = false
      setPending(null)
    }
  }

  const current = cluster.association?.mode === 'manual' ? cluster.association.anilistId : undefined
  const busy = pending !== null

  return (
    <dialog
      ref={dialog}
      className="assoc-dialog"
      aria-labelledby={headingId}
      onClose={() => {
        cancelSearch()
        setResults((r) => (r.phase === 'loading' ? { phase: 'idle' } : r))
        onClose()
      }}
      onPointerDown={(event) => {
        downOnBackdrop.current = dialog.current !== null && onBackdrop(dialog.current, event)
      }}
      onClick={(event) => {
        // 按下和松开都在遮罩上才算点遮罩：在输入框里拖选文字、松手落到遮罩上不该关掉
        if (downOnBackdrop.current && dialog.current && onBackdrop(dialog.current, event)) dialog.current.close()
        downOnBackdrop.current = false
      }}
    >
      <div className="assoc-dialog-body">
        <button type="button" className="icon-button assoc-dialog-close" aria-label="关闭" onClick={() => dialog.current?.close()}>
          <Icon name="close" />
        </button>
        <h2 id={headingId}>选择对应的作品</h2>
        <p className="assoc-hint">选定之后，弹幕按这部作品匹配，看完的集数也记到它上面。本地文件夹：{cluster.title}</p>
        <form
          className="assoc-search"
          role="search"
          onSubmit={(event) => {
            event.preventDefault()
            runSearch(query)
          }}
        >
          <input
            ref={input}
            className="input"
            type="text"
            inputMode="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            aria-label="搜索作品名"
            placeholder="作品名，原名或英文名也可以"
            maxLength={MAX_QUERY_LENGTH}
            enterKeyHint="search"
            autoComplete="off"
          />
          <button type="submit" className="btn btn--sm btn--primary">
            搜索
          </button>
        </form>

        {error && (
          <p className="result result--err" role="alert">
            {error}
          </p>
        )}
        {/* 常驻的播报区：随状态换内容，而不是跟着内容一起挂载（后者读屏常常不念） */}
        <p className="visually-hidden" role="status">
          {announcement(results)}
        </p>
        <SearchResults state={results} current={current} matched={cluster.matched?.anilistId} pending={pending} onChoose={(id) => void choose({ mode: 'manual', anilistId: id })} />

        <footer className="assoc-dialog-footer">
          <p>自己录的视频、目录里找不到的作品：</p>
          <button type="button" className="btn btn--sm" disabled={busy || cluster.association?.mode === 'none'} onClick={() => void choose({ mode: 'none' })}>
            {pending === 'none' ? '正在保存…' : '不是目录里的作品'}
          </button>
        </footer>
      </div>
    </dialog>
  )
}

function announcement(state: SearchState): string {
  if (state.phase === 'loading') return '正在搜索'
  if (state.phase === 'ready') return state.items.length ? `找到 ${state.items.length} 部作品` : '没有找到'
  return ''
}

function SearchResults({ state, current, matched, pending, onChoose }: {
  state: SearchState
  current?: number
  matched?: number
  pending: number | 'none' | null
  onChoose: (anilistId: number) => void
}) {
  if (state.phase === 'idle') return <p className="assoc-hint">输入作品名搜索。</p>
  if (state.phase === 'loading') return <p className="result result--dim">正在搜索 …</p>
  if (state.phase === 'error') {
    return (
      <p className="result result--err" role="alert">
        {state.message}
      </p>
    )
  }
  if (state.items.length === 0) return <p className="assoc-hint">没有找到。试试原名、英文名，或去掉季数、字幕组之类的字样。</p>
  return (
    <ul className="assoc-results" aria-busy={pending !== null}>
      {state.items.map((media) => (
        <li key={media.id}>
          <ResultButton media={media} current={media.id === current} matched={media.id === matched} pending={pending} onChoose={onChoose} />
        </li>
      ))}
    </ul>
  )
}

function ResultButton({ media, current, matched, pending, onChoose }: {
  media: MediaSummary
  current: boolean
  matched: boolean
  pending: number | 'none' | null
  onChoose: (anilistId: number) => void
}) {
  const [failedSrc, setFailedSrc] = useState<string>()
  const meta = [media.year, media.format && (FORMAT_LABELS[media.format] ?? media.format), media.episodes ? `${media.episodes} 集` : undefined].filter(Boolean).join(' · ')
  const hasCover = media.cover !== undefined && failedSrc !== media.cover
  return (
    <button type="button" className="assoc-result" disabled={pending !== null || current} onClick={() => onChoose(media.id)}>
      <span className="assoc-result-art">{hasCover ? <img src={media.cover} alt="" loading="lazy" onError={() => setFailedSrc(media.cover)} /> : <span aria-hidden="true">流</span>}</span>
      <span className="assoc-result-text">
        <span className="assoc-result-title">{media.title}</span>
        {media.titleNative && media.titleNative !== media.title && <span className="assoc-result-sub">{media.titleNative}</span>}
        {meta && <span className="assoc-result-sub">{meta}</span>}
      </span>
      {current ? <span className="badge">当前</span> : matched ? <span className="badge">自动匹配</span> : null}
      {pending === media.id && <span className="assoc-result-sub">正在保存…</span>}
    </button>
  )
}
