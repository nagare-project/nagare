import { useLayoutEffect } from 'react'
import type { RefObject } from 'react'

/** 横向一行翻到了哪一头 */
export interface ArrowEdges {
  start: boolean
  end: boolean
}

/**
 * 横向一行的前后箭头翻到头时会变成 disabled，浏览器随后把焦点从它身上拿走、掉到 <body>。
 * 在箭头变禁用的那次提交之后、浏览器下一次更新渲染（它在那时才挪走焦点）之前，把焦点交给另一头的箭头 ——
 * 一头到了边，另一头这时一定能翻。不能在算出新边界时当场挪：那一刻另一头的箭头往往还是 disabled
 * （只有两页时，从第一页一下就翻到了最后一页），对它 focus() 什么都不会发生。
 * 两头都到边（内容不再溢出）时不动。
 */
export function useArrowFocus(edges: ArrowEdges, start: RefObject<HTMLElement | null>, end: RefObject<HTMLElement | null>): void {
  useLayoutEffect(() => {
    if (edges.start && edges.end) return
    const active = document.activeElement
    if (active === null) return
    if (edges.end && active === end.current) start.current?.focus({ preventScroll: true })
    else if (edges.start && active === start.current) end.current?.focus({ preventScroll: true })
  }, [edges.start, edges.end, start, end])
}
