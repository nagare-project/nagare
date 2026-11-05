import type { KeyboardEvent } from 'react'

/**
 * 输入法还在组字：Chromium 带 isComposing；Safari 先发 compositionend，随后那次按键的 keyCode 是 229。
 * 组字时的 Enter 是确认候选、Esc 是取消候选，都不该当成提交 / 收起（DESIGN.md「输入 · 搜索框」）。
 */
export function isComposing(event: KeyboardEvent<Element>): boolean {
  return event.nativeEvent.isComposing || event.keyCode === 229
}

/** 搜索框的 onKeyDown：输入法确认候选的那次 Enter 不提交表单 */
export function preventComposingSubmit(event: KeyboardEvent<HTMLInputElement>): void {
  if (event.key === 'Enter' && isComposing(event)) event.preventDefault()
}
