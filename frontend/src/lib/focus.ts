/**
 * 焦点所在的控件被拿掉之后（删掉的那一行、登录后换掉的表单、关掉的横幅），把焦点交给 target，
 * 否则它会掉到 <body> 上，键盘用户下一次 Tab 要从页面最上面重新走。
 *
 * 只在焦点还在 scope 里、或已经掉到 <body> 上时才移：请求要等好几秒，用户这期间去了别处就不抢回来。
 * target 不是可聚焦元素（标题、区块）时临时给它 tabindex="-1"，只能由脚本聚焦、不进 Tab 顺序。
 */
export function handOffFocus(target: HTMLElement | null, scope: HTMLElement | null = null): void {
  if (target === null || !target.isConnected) return
  const active = document.activeElement
  if (active !== null && active !== document.body && scope?.contains(active) !== true) return
  if (target.tabIndex < 0 && !target.hasAttribute('tabindex')) target.setAttribute('tabindex', '-1')
  target.focus()
}

/** 横幅这类页面外壳上的东西关掉之后：焦点交给当前页面的主标题（没有就交给 <main>） */
export function pageHeading(): HTMLElement | null {
  return document.querySelector<HTMLElement>('main h1') ?? document.querySelector<HTMLElement>('main')
}

/** 一对前后翻页箭头：翻到哪一头 / 箭头元素 */
export interface ArrowEdges {
  start: boolean
  end: boolean
}

/**
 * 横向一行的前后箭头翻到头时会变成 disabled，浏览器随即把焦点从它身上拿走、掉到 <body>。
 * 在算出新边界、箭头还没变禁用的那一刻调用：焦点在那个要变禁用的箭头上时交给另一头的箭头 ——
 * 一头到了边，另一头这时一定能翻。两头都到边（内容不再溢出）时不动，交给调用方。
 */
export function keepArrowFocus(edges: ArrowEdges, arrows: { start: HTMLElement | null; end: HTMLElement | null }): void {
  if (edges.start && edges.end) return
  const active = document.activeElement
  if (edges.end && active !== null && active === arrows.end) arrows.start?.focus({ preventScroll: true })
  else if (edges.start && active !== null && active === arrows.start) arrows.end?.focus({ preventScroll: true })
}
