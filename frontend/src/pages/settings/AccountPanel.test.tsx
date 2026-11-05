// @vitest-environment jsdom
import { act, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AnimegoInfo } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { getByRole, getByText, queryByRole } from '../../test/queries'

const endpoints = vi.hoisted(() => ({ animegoLogin: vi.fn(), animegoLogout: vi.fn() }))
vi.mock('../../lib/endpoints', () => endpoints)

import { AccountPanel } from './AccountPanel'

const LOGGED_OUT: AnimegoInfo = { loggedIn: false, baseUrl: 'https://account.example.invalid' }
const LOGGED_IN: AnimegoInfo = { loggedIn: true, email: 'you@example.com', baseUrl: 'https://account.example.invalid' }

/** 父组件的样子：onReload 重新读设置，账号状态跟着变 */
function Harness({ initial, next, onReload }: { initial: AnimegoInfo; next: AnimegoInfo; onReload: () => void }) {
  const [account, setAccount] = useState(initial)
  return (
    <AccountPanel
      account={account}
      onReload={async () => {
        onReload()
        setAccount(next)
      }}
    />
  )
}

/** 受控输入框要走原生 setter 再发 input 事件，React 才认 */
function type(input: HTMLElement, value: string): void {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

async function submitLogin(container: HTMLElement, email: string, password: string): Promise<void> {
  await act(async () => {
    type(getByRole(container, 'textbox', { name: '邮箱' }), email)
    type(container.querySelector<HTMLInputElement>('#account-password')!, password)
  })
  const submit = getByRole(container, 'button', { name: '登录' })
  submit.focus()
  await act(async () => { submit.click() })
}

const mounted: Array<() => Promise<void>> = []
afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.resetAllMocks()
  vi.restoreAllMocks()
})

describe('AccountPanel', () => {
  it('登录成功：发出账号变化事件、刷新设置、清掉密码，焦点落到换上来的「退出登录」', async () => {
    endpoints.animegoLogin.mockResolvedValue({ user: { email: 'you@example.com' } })
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const onReload = vi.fn()
    const changed = vi.fn()
    window.addEventListener('nagare:account-changed', changed)
    const m = await mount(<Harness initial={LOGGED_OUT} next={LOGGED_IN} onReload={onReload} />)
    mounted.push(m.unmount)

    await submitLogin(m.container, '  you@example.com ', 'hunter2')

    expect(endpoints.animegoLogin).toHaveBeenCalledWith('you@example.com', 'hunter2')
    expect(changed).toHaveBeenCalledTimes(1)
    expect(onReload).toHaveBeenCalledTimes(1)
    expect(getByText(m.container, 'you@example.com')).toBeDefined()
    expect(queryByRole(m.container, 'textbox', { name: '邮箱' })).toBeNull()
    expect(document.activeElement).toBe(getByRole(m.container, 'button', { name: '退出登录' }))
    window.removeEventListener('nagare:account-changed', changed)
  })

  it('登录失败：原因写在状态行上，表单与输入都留着，不刷新设置', async () => {
    endpoints.animegoLogin.mockRejectedValue(new Error('邮箱或密码不对'))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const onReload = vi.fn()
    const m = await mount(<Harness initial={LOGGED_OUT} next={LOGGED_IN} onReload={onReload} />)
    mounted.push(m.unmount)

    await submitLogin(m.container, 'you@example.com', 'wrong')

    expect(getByRole(m.container, 'status').textContent).toContain('邮箱或密码不对')
    expect((getByRole(m.container, 'textbox', { name: '邮箱' }) as HTMLInputElement).value).toBe('you@example.com')
    expect(onReload).not.toHaveBeenCalled()
  })

  it('登录中：输入框只读、「登录中」不可用，但焦点留在原处（不掉到 <body>）；失败回来时还在那里，再点就能重试', async () => {
    let reject: (err: Error) => void = () => {}
    endpoints.animegoLogin.mockReturnValueOnce(new Promise((_, r) => { reject = r }))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const m = await mount(<Harness initial={LOGGED_OUT} next={LOGGED_IN} onReload={vi.fn()} />)
    mounted.push(m.unmount)
    await submitLogin(m.container, 'you@example.com', 'wrong')

    const pending = getByRole(m.container, 'button', { name: '登录中 …' }) as HTMLButtonElement
    expect(pending.disabled).toBe(false)
    expect(pending.getAttribute('aria-disabled')).toBe('true')
    expect(document.activeElement).toBe(pending)
    const email = getByRole(m.container, 'textbox', { name: '邮箱' }) as HTMLInputElement
    expect(email.readOnly).toBe(true)
    expect(email.disabled).toBe(false)
    // 请求中再点（或在输入框里回车）不会再发一次
    await act(async () => { pending.click() })
    expect(endpoints.animegoLogin).toHaveBeenCalledTimes(1)

    await act(async () => { reject(new Error('邮箱或密码不对')) })
    const submit = getByRole(m.container, 'button', { name: '登录' })
    expect(submit).toBe(pending)
    expect(submit.hasAttribute('aria-disabled')).toBe(false)
    expect(document.activeElement).toBe(submit)
    expect(email.readOnly).toBe(false)
    endpoints.animegoLogin.mockRejectedValueOnce(new Error('还是不对'))
    await act(async () => { submit.click() })
    expect(endpoints.animegoLogin).toHaveBeenCalledTimes(2)
  })

  it('没填邮箱或密码就提交：不发请求，直接说缺什么', async () => {
    const m = await mount(<Harness initial={LOGGED_OUT} next={LOGGED_IN} onReload={vi.fn()} />)
    mounted.push(m.unmount)

    await submitLogin(m.container, 'you@example.com', '')

    expect(endpoints.animegoLogin).not.toHaveBeenCalled()
    expect(getByRole(m.container, 'status').textContent).toBe('请输入邮箱和密码')
  })

  it('退出登录：刷新设置后换回表单，焦点落到邮箱输入框', async () => {
    endpoints.animegoLogout.mockResolvedValue(undefined)
    const onReload = vi.fn()
    const m = await mount(<Harness initial={LOGGED_IN} next={LOGGED_OUT} onReload={onReload} />)
    mounted.push(m.unmount)
    // 登录之前就要说清会话互斥：已登录时这条说明也一直在
    expect(getByRole(m.container, 'note', { name: /不能同时保持登录/ })).toBeDefined()

    const logout = getByRole(m.container, 'button', { name: '退出登录' })
    logout.focus()
    await act(async () => { logout.click() })

    expect(endpoints.animegoLogout).toHaveBeenCalledTimes(1)
    expect(onReload).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(getByRole(m.container, 'textbox', { name: '邮箱' }))
  })
})
