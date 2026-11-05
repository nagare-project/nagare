// @vitest-environment jsdom
import { act, useRef } from 'react'
import { describe, expect, it } from 'vitest'
import { mount } from '../test/harness'
import { getByRole } from '../test/queries'
import { useArrowFocus } from './useArrowFocus'
import type { ArrowEdges } from './useArrowFocus'

/** 与三处横向一行（海报行、类型标签、即将播出）一样：两颗箭头，翻到哪头哪颗 disabled */
function Arrows({ edges }: { edges: ArrowEdges }) {
  const start = useRef<HTMLButtonElement>(null)
  const end = useRef<HTMLButtonElement>(null)
  useArrowFocus(edges, start, end)
  return (
    <div>
      <button ref={start} type="button" disabled={edges.start}>向前翻页</button>
      <button ref={end} type="button" disabled={edges.end}>向后翻页</button>
      <button type="button">别处</button>
    </div>
  )
}

const prev = (c: HTMLElement) => getByRole(c, 'button', { name: '向前翻页' })
const next = (c: HTMLElement) => getByRole(c, 'button', { name: '向后翻页' })

describe('翻到头时箭头的焦点（useArrowFocus）', () => {
  it('焦点在「向后」上、翻到最后：交给「向前」—— 即使「向前」在同一次提交里才刚变得可点（只有两页）', async () => {
    const { container, rerender, unmount } = await mount(<Arrows edges={{ start: true, end: false }} />)
    next(container).focus()
    await rerender(<Arrows edges={{ start: false, end: true }} />)
    expect(document.activeElement).toBe(prev(container))
    await unmount()
  })

  it('焦点在「向前」上、翻回开头：交给「向后」', async () => {
    const { container, rerender, unmount } = await mount(<Arrows edges={{ start: false, end: false }} />)
    prev(container).focus()
    await rerender(<Arrows edges={{ start: true, end: false }} />)
    expect(document.activeElement).toBe(next(container))
    await unmount()
  })

  it('焦点不在变禁用的那颗上：不动；两头都到边（内容不再溢出）：也不动', async () => {
    const { container, rerender, unmount } = await mount(<Arrows edges={{ start: false, end: false }} />)
    const elsewhere = getByRole(container, 'button', { name: '别处' })
    elsewhere.focus()
    await rerender(<Arrows edges={{ start: false, end: true }} />)
    expect(document.activeElement).toBe(elsewhere)

    await act(async () => prev(container).focus())
    await rerender(<Arrows edges={{ start: true, end: true }} />)
    expect(document.activeElement).toBe(prev(container))
    await unmount()
  })
})
