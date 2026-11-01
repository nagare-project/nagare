import { useEffect, useRef, useState } from 'react'
import type { FocusEvent, FormEvent, KeyboardEvent, MouseEvent } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Icon } from '../ui/Icon'
import { MAX_QUERY_LENGTH } from '../media/CatalogSearch'

/** 输入法还在组字：Chromium 带 isComposing；Safari 先发 compositionend，随后那次按键的 keyCode 是 229 */
function isComposing(event: KeyboardEvent<Element>): boolean {
  return event.nativeEvent.isComposing || event.keyCode === 229
}

interface HeaderSearchProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * 顶栏的作品搜索（DESIGN.md「桌面顶栏 · 搜索」）：圆形放大镜向左展开成输入框，
 * 提交后落到发现页 `/?q=`；Esc 或 ✕ 收起并把焦点还给放大镜，焦点离开搜索框时静默收起。
 *
 * 输入框用 type="text"：type="search" 的第一次 Esc 只清空输入框，收不起来。
 */
export function HeaderSearch({ open, onOpenChange }: HeaderSearchProps) {
  const navigate = useNavigate()
  const [text, setText] = useState('')
  const toggle = useRef<HTMLButtonElement>(null)
  const input = useRef<HTMLInputElement>(null)
  // 收起之后要不要把焦点还给放大镜：放大镜要等重新渲染出来才能聚焦
  const returnFocus = useRef(false)
  // 已经在收起了：输入框随表单卸载时可能再触发一次 blur，不能把「还焦点」改写成 false
  const closing = useRef(false)

  useEffect(() => {
    if (open) {
      closing.current = false
      input.current?.focus({ preventScroll: true })
    } else if (returnFocus.current) {
      returnFocus.current = false
      toggle.current?.focus({ preventScroll: true })
    }
  }, [open])

  function collapse(giveFocusBack: boolean) {
    if (closing.current) return
    closing.current = true
    returnFocus.current = giveFocusBack
    onOpenChange(false)
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const q = text.trim()
    if (q === '') return
    setText('')
    collapse(true)
    void navigate({ to: '/', search: { q } })
  }

  function handleKeyDown(event: KeyboardEvent<HTMLFormElement>) {
    // 输入法正在选字时的 Esc 是取消候选，不是收起搜索
    if (event.key === 'Escape' && !isComposing(event)) {
      event.preventDefault()
      collapse(true)
    }
  }

  function handleInputKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    // 输入法确认候选的那次 Enter 不提交
    if (event.key === 'Enter' && isComposing(event)) event.preventDefault()
  }

  function handleBlur(event: FocusEvent<HTMLFormElement>) {
    const next = event.relatedTarget
    if (next instanceof Node && event.currentTarget.contains(next)) return
    collapse(false)
  }

  function handleMouseDown(event: MouseEvent<HTMLFormElement>) {
    // 按在胶囊上不可聚焦的地方（放大镜、留白）不要让输入框失焦 —— 失焦就会收起
    if (event.target instanceof Element && event.target.closest('input, button') === null) event.preventDefault()
  }

  if (!open) {
    return (
      <button ref={toggle} type="button" className="app-header-icon" aria-label="搜索作品" title="搜索作品"
        onClick={() => onOpenChange(true)}>
        <Icon name="search" size={20} />
      </button>
    )
  }

  return (
    <form className="header-search" role="search" aria-label="搜索作品" onSubmit={handleSubmit} onKeyDown={handleKeyDown} onBlur={handleBlur}
      onMouseDown={handleMouseDown}>
      <span className="header-search-icon" aria-hidden="true"><Icon name="search" size={18} /></span>
      <input
        ref={input}
        className="header-search-input"
        type="text"
        value={text}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={handleInputKeyDown}
        placeholder="搜索作品名"
        aria-label="作品名"
        maxLength={MAX_QUERY_LENGTH}
        enterKeyHint="search"
        autoComplete="off"
      />
      <button type="button" className="header-search-close" aria-label="收起搜索" onClick={() => collapse(true)}>
        <Icon name="close" size={16} />
      </button>
    </form>
  )
}
