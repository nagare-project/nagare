// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { mount } from '../test/harness'
import { getByRole } from '../test/queries'
import { inFlight } from './busy'

describe('请求进行中的按钮（inFlight）', () => {
  it('不忙：照常可点，不带 aria-disabled', async () => {
    const onClick = vi.fn()
    const { container, unmount } = await mount(<button type="button" {...inFlight(false, onClick)}>保存</button>)
    const button = getByRole(container, 'button', { name: '保存' }) as HTMLButtonElement
    expect(button.hasAttribute('aria-disabled')).toBe(false)
    await act(async () => button.click())
    expect(onClick).toHaveBeenCalledTimes(1)
    await unmount()
  })

  it('忙：aria-disabled 而不是 disabled（焦点留得住），点击被吞掉并取消默认动作', async () => {
    const onClick = vi.fn()
    const { container, unmount } = await mount(<button type="button" {...inFlight(true, onClick)}>保存中</button>)
    const button = getByRole(container, 'button', { name: '保存中' }) as HTMLButtonElement
    expect(button.getAttribute('aria-disabled')).toBe('true')
    expect(button.disabled).toBe(false)
    button.focus()
    const click = new MouseEvent('click', { bubbles: true, cancelable: true })
    await act(async () => { button.dispatchEvent(click) })
    expect(onClick).not.toHaveBeenCalled()
    expect(click.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(button)
    await unmount()
  })

  it('忙时表单的提交按钮：浏览器对它派发的那一下点击（回车的隐式提交也是这样）被取消，表单不提交', async () => {
    const onSubmit = vi.fn((event: { preventDefault: () => void }) => event.preventDefault())
    const { container, unmount } = await mount(
      <form onSubmit={onSubmit}>
        <input aria-label="密码" type="password" />
        <button type="submit" {...inFlight(true)}>登录中</button>
      </form>,
    )
    await act(async () => getByRole(container, 'button', { name: '登录中' }).click())
    expect(onSubmit).not.toHaveBeenCalled()
    await unmount()
  })
})
