// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { PlayerStatus, SyncFailure } from '../../lib/endpoints'
import { describeSyncRetry } from '../../lib/syncRetry'
import { getByRole, queryByRole } from '../../test/queries'
import { mount } from '../../test/harness'
import { SyncAlert } from './SyncAlert'

function statusWith(sync: SyncFailure): PlayerStatus {
  return { playing: false, sync }
}

const FAILED: SyncFailure = {
  state: 'failed',
  title: '葬送的芙莉莲',
  episode: 3,
  reason: '服务暂不可达',
  recovery: '确认网络后点「重试」；下次启动 nagare 时也会自动再试',
}

function retryButton(container: HTMLElement): HTMLButtonElement | undefined {
  return (queryByRole(container, 'button', { name: /^重试/ }) as HTMLButtonElement | null) ?? undefined
}

describe('SyncAlert', () => {
  it('还有会再试的集时给「重试」，点了调 onRetry，进行中按钮禁用、防重复点', async () => {
    let finish: () => void = () => {}
    const onRetry = vi.fn(() => new Promise<void>((resolve) => { finish = resolve }))
    const { container, unmount } = await mount(
      <SyncAlert status={statusWith({ ...FAILED, pending: 1 })} onRetry={onRetry} />,
    )

    const button = retryButton(container)
    expect(button, '会再试的失败要能当场重试').toBeDefined()
    button!.focus()
    await act(async () => { button!.click() })
    expect(onRetry).toHaveBeenCalledTimes(1)
    // 进行中不可用，但不是 disabled：焦点留在按钮上
    expect(retryButton(container)?.getAttribute('aria-disabled')).toBe('true')
    expect(document.activeElement).toBe(retryButton(container))
    expect(retryButton(container)?.textContent).toContain('重试中')
    await act(async () => { retryButton(container)!.click() })
    expect(onRetry).toHaveBeenCalledTimes(1)

    await act(async () => { finish() })
    expect(retryButton(container)?.hasAttribute('aria-disabled')).toBe(false)
    expect(document.activeElement).toBe(retryButton(container))
    await unmount()
  })

  it('有待补记的集时说出个数，以及登录后、下次启动都会自动补上', async () => {
    const { container, unmount } = await mount(
      <SyncAlert status={statusWith({ ...FAILED, pending: 3 })} onRetry={vi.fn(async () => {})} />,
    )
    const text = getByRole(container, 'status').textContent ?? ''
    expect(text).toContain('共有 3 集等着补记')
    expect(text).toContain('登录后或下次启动 nagare 时会自动补上')
    // 恢复动作不再说「重看这一集才会再试」
    expect(text).not.toContain('重看')
    await unmount()
  })

  it('补发不会再试的（集号换算不出来，pending 为 0）不给按钮，只留手动标记的恢复动作', async () => {
    const { container, unmount } = await mount(
      <SyncAlert
        status={statusWith({ ...FAILED, reason: '算不出文件里的第 38 集是作品的第几集', recovery: '先确认它是作品的第几集，再手动标记', pending: 0 })}
        onRetry={vi.fn(async () => {})}
      />,
    )
    expect(retryButton(container)).toBeUndefined()
    expect(getByRole(container, 'status').textContent).toContain('→ 先确认它是作品的第几集，再手动标记')
    await unmount()
  })

  it('没有失败时什么都不画', async () => {
    const { container, unmount } = await mount(<SyncAlert status={{ playing: false }} onRetry={vi.fn(async () => {})} />)
    expect(queryByRole(container, 'status')).toBeNull()
    await unmount()
  })
})

describe('describeSyncRetry', () => {
  it('补完了、到了单轮上限、没补成、没东西可补各有一句话', () => {
    expect(describeSyncRetry({ synced: 2, remaining: 0 })).toEqual({ tone: 'ok', text: '补记上 2 集，都记到账号了' })
    expect(describeSyncRetry({ synced: 10, remaining: 3 }).text).toBe('补记上 10 集，还有 3 集没记上：再点一次「重试」，登录后或下次启动时也会自动补记')
    expect(describeSyncRetry({ synced: 0, remaining: 1 })).toEqual({ tone: 'dim', text: '这次没能补记，还有 1 集等着：登录后或下次启动时会再试' })
    expect(describeSyncRetry({ synced: 0, remaining: 0 }).text).toBe('没有需要补记的集')
  })

  it('没补完时原样说出原因（没登录、连不上），补上了几集也一起说', () => {
    expect(describeSyncRetry({ synced: 0, remaining: 2, reason: '还没有登录账号' })).toEqual({ tone: 'err', text: '还没有登录账号' })
    expect(describeSyncRetry({ synced: 1, remaining: 1, reason: '连不上' }).text).toBe('补记上 1 集；其余没补完：连不上')
  })
})
