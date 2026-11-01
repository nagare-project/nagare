import type { MouseEvent, PointerEvent } from 'react'

/**
 * 这一下按在原生模态 <dialog> 的框体之外（遮罩上）。
 * 框体内的滚动条也算 target === dialog，所以除了 target 还要按坐标判断。
 *
 * 点遮罩关闭要求【按下与松开都】在框体之外：在框体里按下、拖到遮罩上松手（拖选文字）
 * 不能把对话框关掉 —— 调用方在 pointerdown 记一次、click 再判一次。
 */
export function isBackdropEvent(dialog: HTMLDialogElement, event: MouseEvent | PointerEvent): boolean {
  if (event.target !== dialog) return false
  const r = dialog.getBoundingClientRect()
  return event.clientX < r.left || event.clientX > r.right || event.clientY < r.top || event.clientY > r.bottom
}
