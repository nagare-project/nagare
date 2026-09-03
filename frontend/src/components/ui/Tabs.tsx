import { createContext, useContext, useId } from 'react'
import type { ReactNode } from 'react'

/**
 * 分档标签（复合组件）。
 *
 * 现在有两处在手搓这个：/lists 的五档观看状态、/discover 的动画/放送表。
 * 两处各写了一遍「哪个是当前档、点了怎么切、aria-current 怎么标」，
 * 而键盘左右箭头切换两边都没做。
 *
 * 收成复合组件之后，选中态由 Context 持有，调用方只写结构：
 *
 *   <Tabs value={s} onChange={setS} label="观看状态">
 *     <Tab value="watching">在看<TabCount n={5} /></Tab>
 *   </Tabs>
 *
 * 键盘行为按 WAI-ARIA 的 tablist 惯例：左右箭头在档之间移动、Home/End 跳两端。
 * 手写版本没有这个，而对只用键盘的人来说，没有它这排标签就得一个个 Tab 键过去。
 */

interface TabsContextValue {
  value: string
  onChange: (next: string) => void
  /** 同一组标签共享的 id 前缀，用于生成稳定的 tab id */
  groupId: string
}

const TabsContext = createContext<TabsContextValue | null>(null)

function useTabs(): TabsContextValue {
  const ctx = useContext(TabsContext)
  if (ctx === null) throw new Error('Tab 必须放在 Tabs 里面')
  return ctx
}

export interface TabsProps {
  value: string
  onChange: (next: string) => void
  /** 读屏念出来的这组标签是什么 */
  label: string
  children: ReactNode
  /** 居中排布（发现页那种压在横幅下方的形态） */
  center?: boolean
}

export function Tabs({ value, onChange, label, children, center = false }: TabsProps) {
  const groupId = useId()

  // 左右箭头在同组标签间移动焦点并切换。取 DOM 里的实际顺序而不是维护一份
  // 列表 —— 后者会和 JSX 的真实顺序漂移。
  function handleKeyDown(e: React.KeyboardEvent<HTMLElement>): void {
    const keys = ['ArrowLeft', 'ArrowRight', 'Home', 'End']
    if (!keys.includes(e.key)) return
    const tabs = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]')]
    if (tabs.length === 0) return
    const current = tabs.findIndex((t) => t === document.activeElement)
    let next: number
    switch (e.key) {
      case 'ArrowLeft':
        next = current <= 0 ? tabs.length - 1 : current - 1
        break
      case 'ArrowRight':
        next = current === -1 || current === tabs.length - 1 ? 0 : current + 1
        break
      case 'Home':
        next = 0
        break
      default:
        next = tabs.length - 1
    }
    e.preventDefault()
    tabs[next]?.focus()
    tabs[next]?.click()
  }

  return (
    <TabsContext.Provider value={{ value, onChange, groupId }}>
      <nav
        className={center ? 'tabs tabs--center' : 'tabs'}
        role="tablist"
        aria-label={label}
        onKeyDown={handleKeyDown}
      >
        {children}
      </nav>
    </TabsContext.Provider>
  )
}

export interface TabProps {
  value: string
  children: ReactNode
}

export function Tab({ value, children }: TabProps) {
  const ctx = useTabs()
  const selected = ctx.value === value
  return (
    <button
      type="button"
      role="tab"
      id={`${ctx.groupId}-${value}`}
      aria-selected={selected}
      // 未选中的档退出 Tab 键序列：一排标签在 tablist 里算【一个】停靠点，
      // 进去之后用箭头走。这是 WAI-ARIA 对 tablist 的规定，也让 Tab 键
      // 不会被十几个档淹没。
      tabIndex={selected ? 0 : -1}
      className={selected ? 'tab tab--on' : 'tab'}
      onClick={() => ctx.onChange(value)}
    >
      {children}
    </button>
  )
}

/** 标签右侧的计数，弱化一档 */
export function TabCount({ n }: { n: number }) {
  return <span className="tab-count">{n}</span>
}
