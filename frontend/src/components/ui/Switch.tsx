/**
 * 开关。
 *
 * 用 <button role="switch"> 而不是 <input type="checkbox">：视觉是自绘的，
 * checkbox 要么被 appearance:none 抹掉后失去键盘语义，要么得盖一层假的。
 * button + role/aria-checked 是这种自绘控件的标准做法，空格与回车天然可用。
 *
 * aria-label 是必填 prop：一个只有小圆点的开关，不给标签读屏什么都读不出来，
 * 而手写时这一条几乎总是被忘掉。
 */
export interface SwitchProps {
  checked: boolean
  onChange: (next: boolean) => void
  /** 读屏念出来的名字，必填 */
  label: string
  disabled?: boolean
  extraClass?: string
}

export function Switch({ checked, onChange, label, disabled = false, extraClass }: SwitchProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      className={['switch', extraClass ?? ''].filter(Boolean).join(' ')}
      onClick={() => onChange(!checked)}
    >
      <span className="switch-knob" />
    </button>
  )
}
