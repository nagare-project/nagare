import type { IconName } from '../ui/Icon'

/** 顶栏与抽屉共用的主导航（DESIGN.md「桌面顶栏」：发现 · 媒体库 · 放送表 · 季度 · 我的追番） */
export interface NavItem {
  to: '/' | '/library' | '/schedule' | '/seasonal' | '/lists'
  label: string
  icon: IconName
}

export const PRIMARY_NAV: readonly NavItem[] = [
  { to: '/', label: '发现', icon: 'compass' },
  { to: '/library', label: '媒体库', icon: 'folder' },
  { to: '/schedule', label: '放送表', icon: 'calendar' },
  { to: '/seasonal', label: '季度', icon: 'grid' },
  { to: '/lists', label: '我的追番', icon: 'lists' },
]

/**
 * 当前页高亮的匹配方式。
 * - includeSearch:false：`/?q=` 的作品搜索也算在「发现」里，不能因为带了查询参数就不高亮；
 * - `/` 必须精确匹配，否则每一页都会把「发现」标成当前页。
 */
export function navActiveOptions(to: NavItem['to']): { exact: boolean; includeSearch: false } {
  return { exact: to === '/', includeSearch: false }
}
