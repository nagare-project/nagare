/**
 * 按角色与文字找元素（测试用，jsdom）。
 *
 * 项目不引 @testing-library（见 harness.tsx），但测试照样应该像用户与读屏那样找东西：
 * 「名字叫『播放第 2 集』的按钮」，而不是「class 是 hero-btn 的那个 button」——
 * 换样式不该让行为测试红，读屏念不出来的名字也不该让测试绿。
 *
 * 这里是 ARIA 角色与可访问名称的近似实现，只覆盖本项目界面里用得到的那部分：
 * - 角色：常用元素的隐含角色 + 显式 role。
 * - 名字：aria-labelledby > aria-label > 内容文字（去掉 aria-hidden，图片取 alt）> 表单控件的 <label> > title。
 * - 默认跳过读屏碰不到的元素：hidden / aria-hidden / inert 的子树、没打开的 <dialog>、
 *   收起的 <details> 里除 <summary> 以外的内容。
 */

export type Role =
  | 'alert' | 'button' | 'checkbox' | 'combobox' | 'dialog' | 'group' | 'heading' | 'img' | 'link'
  | 'list' | 'listitem' | 'main' | 'menu' | 'menuitem' | 'navigation' | 'note' | 'progressbar'
  | 'region' | 'status' | 'tab' | 'tablist' | 'tabpanel' | 'textbox'

export interface RoleOptions {
  /** 可访问名称：字符串是全等（空白折叠后），正则是匹配 */
  name?: string | RegExp
  /** 只用于 heading：h1–h6 的级别 */
  level?: number
  /** true 时连读屏碰不到的元素也算（默认不算） */
  hidden?: boolean
}

/** 每个角色的隐含元素（显式 role 另算） */
const IMPLICIT: Record<Role, string> = {
  alert: '',
  button: 'button, input[type="button"], input[type="submit"], input[type="reset"]',
  checkbox: 'input[type="checkbox"]',
  combobox: 'select:not([multiple])',
  dialog: 'dialog',
  group: 'details, fieldset, optgroup',
  heading: 'h1, h2, h3, h4, h5, h6',
  img: 'img:not([alt=""])',
  link: 'a[href], area[href]',
  list: 'ul, ol, menu',
  listitem: 'li',
  main: 'main',
  menu: '',
  menuitem: '',
  navigation: 'nav',
  note: '',
  progressbar: 'progress',
  region: 'section[aria-label], section[aria-labelledby]',
  status: 'output',
  tab: '',
  tablist: '',
  tabpanel: '',
  textbox: 'textarea, input:not([type]), input[type="text"], input[type="search"], input[type="email"], input[type="url"], input[type="tel"]',
}

const collapse = (text: string) => text.replace(/\s+/g, ' ').trim()

/** 这个元素读屏碰不到：自己或祖先被藏起来了 */
export function isInaccessible(element: Element): boolean {
  for (let node: Element | null = element; node !== null; node = node.parentElement) {
    if (node.hasAttribute('hidden') || node.getAttribute('aria-hidden') === 'true' || node.hasAttribute('inert')) return true
    if (node instanceof HTMLElement && node.style.display === 'none') return true
    if (node.tagName === 'DIALOG' && !node.hasAttribute('open')) return true
    const parent = node.parentElement
    if (parent?.tagName === 'DETAILS' && !parent.hasAttribute('open') && node.tagName !== 'SUMMARY') return true
  }
  return false
}

/** 子树里读屏会念的文字：去掉 aria-hidden 的部分，图片念 alt */
function contentText(node: Node): string {
  if (node.nodeType === Node.TEXT_NODE) return node.textContent ?? ''
  if (!(node instanceof Element)) return ''
  if (node.getAttribute('aria-hidden') === 'true' || node.hasAttribute('hidden')) return ''
  if (node.tagName === 'IMG') return node.getAttribute('alt') ?? ''
  return [...node.childNodes].map(contentText).join('')
}

function labelledBy(element: Element): string | null {
  const ids = element.getAttribute('aria-labelledby')
  if (ids === null) return null
  const doc = element.ownerDocument
  return ids.split(/\s+/).map((id) => doc.getElementById(id)).filter((el): el is HTMLElement => el !== null).map(contentText).join(' ')
}

function controlLabel(element: Element): string | null {
  if (!(element instanceof HTMLInputElement || element instanceof HTMLSelectElement || element instanceof HTMLTextAreaElement)) return null
  const labels = element.labels ? [...element.labels] : []
  if (labels.length === 0) return null
  // 包住控件的 <label>：控件自己的值不算名字的一部分
  return labels.map((label) => [...label.childNodes].filter((child) => child !== element && !(child instanceof Element && child.contains(element))).map(contentText).join('')).join(' ')
}

/** 可访问名称（近似 accname 1.2，够本项目的界面用） */
export function accessibleName(element: Element): string {
  const byIds = labelledBy(element)
  if (byIds !== null && collapse(byIds) !== '') return collapse(byIds)
  const label = element.getAttribute('aria-label')
  if (label !== null && collapse(label) !== '') return collapse(label)
  if (element.tagName === 'IMG') return collapse(element.getAttribute('alt') ?? '')
  const control = controlLabel(element)
  if (control !== null && collapse(control) !== '') return collapse(control)
  const isField = element instanceof HTMLInputElement || element instanceof HTMLSelectElement || element instanceof HTMLTextAreaElement
  const text = isField ? '' : collapse(contentText(element))
  if (text !== '') return text
  return collapse(element.getAttribute('title') ?? (element instanceof HTMLInputElement ? element.placeholder : ''))
}

function matches(actual: string, expected: string | RegExp): boolean {
  return typeof expected === 'string' ? actual === collapse(expected) : expected.test(actual)
}

function headingLevel(element: Element): number {
  const explicit = Number(element.getAttribute('aria-level'))
  if (Number.isInteger(explicit) && explicit > 0) return explicit
  const tag = /^H([1-6])$/.exec(element.tagName)
  return tag ? Number(tag[1]) : 2
}

/** 所有符合角色（与名字）的元素，按文档顺序 */
export function getAllByRole(root: ParentNode, role: Role, options: RoleOptions = {}): HTMLElement[] {
  const selectors = [`[role="${role}"]`, IMPLICIT[role]].filter(Boolean).join(', ')
  return [...root.querySelectorAll<HTMLElement>(selectors)].filter((element) => {
    const explicit = element.getAttribute('role')
    // 显式写了别的角色（role="tab" 的 button、role="presentation" 的 li）就不是隐含角色了
    if (explicit !== null && explicit !== role) return false
    if (!options.hidden && isInaccessible(element)) return false
    if (options.level !== undefined && headingLevel(element) !== options.level) return false
    return options.name === undefined || matches(accessibleName(element), options.name)
  })
}

function describe(role: Role, options: RoleOptions): string {
  return `角色 ${role}${options.name === undefined ? '' : `、名字 ${String(options.name)}`}`
}

/** 至多一个；一个都没有时返回 null，多于一个时抛错（找得不够准） */
export function queryByRole(root: ParentNode, role: Role, options: RoleOptions = {}): HTMLElement | null {
  const found = getAllByRole(root, role, options)
  if (found.length > 1) throw new Error(`${describe(role, options)} 找到 ${found.length} 个：${found.map(accessibleName).join(' | ')}`)
  return found[0] ?? null
}

/** 恰好一个，否则抛错并列出同角色的候选（方便看名字差在哪） */
export function getByRole(root: ParentNode, role: Role, options: RoleOptions = {}): HTMLElement {
  const found = queryByRole(root, role, options)
  if (found === null) {
    const candidates = getAllByRole(root, role, { ...options, name: undefined }).map(accessibleName)
    throw new Error(`没有找到${describe(role, options)}；同角色的有：${candidates.join(' | ') || '（无）'}`)
  }
  return found
}

/**
 * 自己的文字（空白折叠后）符合的最深一层元素：字符串是全等，正则是匹配。
 * 默认跳过读屏碰不到的元素。
 */
export function getAllByText(root: ParentNode, text: string | RegExp, options: { hidden?: boolean } = {}): HTMLElement[] {
  const all = [...root.querySelectorAll<HTMLElement>('*')].filter((element) => {
    if (['SCRIPT', 'STYLE'].includes(element.tagName)) return false
    if (!options.hidden && isInaccessible(element)) return false
    return matches(collapse(element.textContent ?? ''), text)
  })
  // 只留最深的那层：父元素的文字也会「符合」，但用户读到的是子元素那一句
  return all.filter((element) => !all.some((other) => other !== element && element.contains(other)))
}

export function getByText(root: ParentNode, text: string | RegExp, options: { hidden?: boolean } = {}): HTMLElement {
  const found = getAllByText(root, text, options)
  if (found.length !== 1) throw new Error(`文字 ${String(text)} 找到 ${found.length} 处`)
  return found[0]!
}

export function queryByText(root: ParentNode, text: string | RegExp, options: { hidden?: boolean } = {}): HTMLElement | null {
  const found = getAllByText(root, text, options)
  if (found.length > 1) throw new Error(`文字 ${String(text)} 找到 ${found.length} 处`)
  return found[0] ?? null
}
