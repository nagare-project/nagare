import { useState } from 'react'
import type { FormEvent, Ref } from 'react'
import { preventComposingSubmit } from '../../lib/ime'
import { Icon } from '../ui/Icon'
import './search.css'

export interface SearchFormProps {
  /** 初始关键词（来自 URL ?q=）；父组件用 key={q} 让 URL 变化时重置输入 */
  initialQuery: string
  /** 提交（Enter / 按钮）；已 trim，空串表示清空搜索 */
  onSubmit: (query: string) => void
  /** 搜索在途：按钮文案变化，但不禁用 —— 用户随时可以改关键词重搜 */
  busy?: boolean
  autoFocus?: boolean
  /** 输入框的 ref：选集弹窗关掉而原触发按钮已禁用时，焦点回落到这里 */
  inputRef?: Ref<HTMLInputElement>
}

/**
 * 磁力关键词输入 + 提交（DESIGN.md「输入 · 搜索框」：胶囊形、左边留给放大镜、16px 字）。
 * 不做任何联想 / 推荐：源是用户自己的，关键词也是。
 */
export function SearchForm({ initialQuery, onSubmit, busy = false, autoFocus = false, inputRef }: SearchFormProps) {
  const [value, setValue] = useState(initialQuery)

  function handleSubmit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault()
    onSubmit(value.trim())
  }

  return (
    <form className="search-form" role="search" aria-label="磁力搜索" onSubmit={handleSubmit}>
      <span className="search-field">
        <span className="search-field-icon" aria-hidden="true">
          <Icon name="search" size={18} />
        </span>
        <input
          ref={inputRef}
          className="search-input"
          type="text"
          inputMode="search"
          name="q"
          value={value}
          onChange={(event) => setValue(event.target.value)}
          onKeyDown={preventComposingSubmit}
          placeholder="输入关键词，回车搜索"
          aria-label="搜索关键词"
          autoFocus={autoFocus}
          spellCheck={false}
          autoComplete="off"
          enterKeyHint="search"
        />
      </span>
      <button type="submit" className="btn btn--primary search-submit">
        {busy ? '搜索中 …' : '搜索'}
      </button>
    </form>
  )
}
