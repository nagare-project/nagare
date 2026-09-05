// @vitest-environment jsdom
import { act } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { CollectionProvider, useCollection } from './CollectionContext'
import { MediaListEditor } from './MediaListEditor'
import { fetchCollection, saveCollection } from '../../lib/catalog'
import type { CollectionData } from '../../lib/catalog'

vi.mock('../../lib/catalog', async original => ({ ...await original<typeof import('../../lib/catalog')>(), fetchCollection: vi.fn(), saveCollection: vi.fn(), deleteCollection: vi.fn() }))
const empty: CollectionData = { loggedIn: true, entries: [] }
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')
function Status() { const c = useCollection(); return <span data-collection>{String(c.loggedIn)}:{c.entries.length}:{c.entries[0]?.currentEpisode}</span> }
beforeEach(() => { vi.mocked(fetchCollection).mockReset().mockResolvedValue(empty); vi.mocked(saveCollection).mockReset() })

async function input(element: HTMLInputElement, value: string) {
 await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value); element.dispatchEvent(new Event('input', { bubbles: true })) })
}
describe('账号收藏', () => {
 it('保存失败保留编辑输入，重新读取云端状态后允许重试', async () => {
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.open = true } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { this.open = false; this.dispatchEvent(new Event('close')) } })
  const { container, unmount } = await mount(<CollectionProvider><Status /><MediaListEditor media={{ id: 1, title: 'Anime', episodes: 12, watched: 0, genres: [] }} /></CollectionProvider>)
  try {
   await act(async () => container.querySelector<HTMLButtonElement>('.media-list-trigger')!.click())
   const progress = container.querySelectorAll<HTMLInputElement>('input[type=number]')[1]!
   await input(progress, '4')
   vi.mocked(saveCollection).mockRejectedValueOnce(new Error('离线'))
   await act(async () => container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
   expect(container.querySelector<HTMLDialogElement>('dialog')!.open).toBe(true)
   expect(progress.value).toBe('4'); expect(container.textContent).toContain('离线')
   expect(saveCollection).toHaveBeenCalledWith(1, expect.objectContaining({ progress: 4, status: 'plan_to_watch', score: null }))
   vi.mocked(fetchCollection).mockResolvedValue({ loggedIn: true, entries: [{ anilistId: 1, status: 'plan_to_watch', currentEpisode: 4, score: null, titleRomaji: 'Anime', episodes: 12, paused: false, startedAt: '', completedAt: '', repeat: 0 }] })
   await act(async () => container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
   expect(container.querySelector<HTMLDialogElement>('dialog')!.open).toBe(false)
   expect(container.querySelector('[data-collection]')?.textContent).toBe('true:1:4')
  } finally {
   await unmount()
   if (show) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', show); else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal')
   if (close) Object.defineProperty(HTMLDialogElement.prototype, 'close', close); else Reflect.deleteProperty(HTMLDialogElement.prototype, 'close')
  }
 })
 it('退出登录后旧请求不能把上一账号的收藏重新显示出来', async () => {
  let resolve!: (data: CollectionData) => void
  vi.mocked(fetchCollection).mockImplementationOnce(() => new Promise(done => { resolve = done }))
  const { container, unmount } = await mount(<CollectionProvider><Status /></CollectionProvider>)
  vi.mocked(fetchCollection).mockResolvedValue({ loggedIn: false, entries: [] })
  await act(async () => window.dispatchEvent(new Event('nagare:account-changed')))
  await act(async () => resolve({ loggedIn: true, entries: [] }))
  expect(container.querySelector('[data-collection]')?.textContent).toBe('false:0:')
  await unmount()
 })
})
