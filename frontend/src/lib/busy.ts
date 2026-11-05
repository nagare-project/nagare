import type { MouseEvent } from 'react'

/** inFlight 给按钮的两个属性 */
export interface InFlightProps<T extends Element> {
  'aria-disabled': true | undefined
  onClick: (event: MouseEvent<T>) => void
}

/**
 * 请求进行中的按钮（DESIGN.md「无障碍」）：不写 disabled。
 *
 * 焦点所在的按钮一变成 disabled，浏览器就把焦点从它身上拿走、掉到 <body>：键盘用户下一次 Tab
 * 要从页面最上面重新走，读屏也丢了位置（删文件夹、登录失败都是这样）。改成 aria-disabled：
 * 焦点留在原处，读屏念「不可用」，这一下点击 —— 包括回车 / 空格、表单里在输入框按回车触发的
 * 隐式提交（浏览器是对默认按钮派发一次 click）—— 在这里吞掉。
 *
 *   <button type="button" {...inFlight(busy, () => void handleSave())}>保存</button>
 *
 * 样子由样式表里与 :disabled 并列的 [aria-disabled='true'] 给。
 * 提交按钮另外在 onSubmit 开头判一次 busy：不经过点击的提交（requestSubmit）这里拦不住。
 * 与请求无关、本来就不能点的状态（翻到头的箭头、没填的表单）照旧用 disabled。
 */
export function inFlight<T extends Element = HTMLButtonElement>(busy: boolean, onClick?: (event: MouseEvent<T>) => void): InFlightProps<T> {
  return {
    'aria-disabled': busy ? true : undefined,
    onClick: (event) => {
      if (busy) {
        event.preventDefault()
        return
      }
      onClick?.(event)
    },
  }
}
