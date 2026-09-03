/**
 * UI 原子件层。
 *
 * 存在的理由：改造前全仓 145 处直接写 className（`.btn` 42、`.result` 44、
 * `.panel` 28…），意图散在字符串里，而每个原子件都有一两条「手写时几乎
 * 总会漏、漏了又只对少数人可见」的细节 —— 输入框的标签、开关的 aria-label、
 * 面板与标题的关联、状态行的 live region、标签组的箭头键导航。
 * 封一层的主要收益是这些，不是省几个字符。
 *
 * 约束：原子件渲染的类名与手写时期【逐字一致】，样式表与既有测试都不用动。
 */
export { Badge } from './Badge'
export type { BadgeProps, BadgeTone } from './Badge'
export { Button } from './Button'
export type { ButtonIntent, ButtonProps } from './Button'
export { Input } from './Input'
export type { InputProps } from './Input'
export { Panel } from './Panel'
export type { PanelProps } from './Panel'
export { Result } from './Result'
export type { ResultProps, ResultTone } from './Result'
export { Switch } from './Switch'
export type { SwitchProps } from './Switch'
export { Tab, TabCount, Tabs } from './Tabs'
export type { TabProps, TabsProps } from './Tabs'
