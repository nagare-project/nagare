// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { PlayerStatus, SyncFailure } from '../../lib/endpoints'
import { describeSyncRetry } from '../../lib/syncRetry'
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
  return [...container.querySelectorAll('button')].find((b) => b.textContent?.includes('重试'))
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
    await act(async () => { button!.click() })
    expect(onRetry).toHaveBeenCalledTimes(1)
    expect(retryButton(container)?.disabled).toBe(true)
    expect(retryButton(container)?.textContent).toContain('重试中')

    await act(async () => { finish() })
    expect(retryButton(container)?.disabled).toBe(false)
    await unmount()
  })

  it('有待补记的集时说出个数和「下次启动会自动补上」', async () => {
    const { container, unmount } = await mount(
      <SyncAlert status={statusWith({ ...FAILED, pending: 3 })} onRetry={vi.fn(async () => {})} />,
    )
    const text = container.textContent ?? ''
    expect(text).toContain('共有 3 集等着补记')
    expect(text).toContain('下次启动')
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
    expect(container.querySelector('.alert-warn-recovery')?.textContent).toContain('手动标记')
    await unmount()
  })

  it('没有失败时什么都不画', async () => {
    const { container, unmount } = await mount(<SyncAlert status={{ playing: false }} onRetry={vi.fn(async () => {})} />)
    expect(container.querySelector('.alert-warn')).toBeNull()
    await unmount()
  })
})

describe('describeSyncRetry', () => {
  it('补完了、到了单轮上限、没补成、没东西可补各有一句话', () => {
    expect(describeSyncRetry({ synced: 2, remaining: 0 })).toEqual({ tone: 'ok', text: '补记上 2 集，都记到账号了' })
    expect(describeSyncRetry({ synced: 10, remaining: 3 }).text).toContain('还有 3 集没记上')
    expect(describeSyncRetry({ synced: 0, remaining: 1 }).tone).toBe('dim')
    expect(describeSyncRetry({ synced: 0, remaining: 0 }).text).toBe('没有需要补记的集')
  })

  it('没补完时原样说出原因（没登录、连不上），补上了几集也一起说', () => {
    expect(describeSyncRetry({ synced: 0, remaining: 2, reason: '还没有登录账号' })).toEqual({ tone: 'err', text: '还没有登录账号' })
    expect(describeSyncRetry({ synced: 1, remaining: 1, reason: '连不上' }).text).toBe('补记上 1 集；其余没补完：连不上')
  })
})
