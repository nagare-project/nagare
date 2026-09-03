import type { InputHTMLAttributes } from 'react'

/**
 * 文本输入。类名与手写时期一致（`.input`），样式表不用改。
 *
 * 这一层的价值不在样式，在于把「输入框必须有标签」变成结构上的要求：
 * label 是必填 prop，id 由调用方给，两者在这里绑死。手写时 `<input>`
 * 缺 label 不会报任何错，只有读屏用户会撞上「未标记的编辑框」。
 */
export interface InputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'className' | 'id'> {
  id: string
  /** 可见标签文字；确实不该可见时传 visuallyHidden */
  label: string
  /** 标签视觉隐藏但读屏可达（搜索框这类标题已说明用途的场景） */
  visuallyHidden?: boolean
  /** 标签的内联样式，通常传 theme 的 label */
  labelStyle?: React.CSSProperties
  extraClass?: string
}

export function Input({
  id,
  label,
  visuallyHidden = false,
  labelStyle,
  extraClass,
  ...rest
}: InputProps) {
  return (
    <>
      <label
        htmlFor={id}
        className={visuallyHidden ? 'visually-hidden' : undefined}
        style={visuallyHidden ? undefined : labelStyle}
      >
        {label}
      </label>
      <input id={id} className={['input', extraClass ?? ''].filter(Boolean).join(' ')} {...rest} />
    </>
  )
}
