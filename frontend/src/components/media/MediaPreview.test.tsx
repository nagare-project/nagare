// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { fetchLibrary, playFile } from '../../lib/endpoints'
import { MediaPreview } from './MediaPreview'

vi.mock('../../lib/catalog', async importOriginal => ({ ...await importOriginal<typeof import('../../lib/catalog')>(), fetchMedia: vi.fn(async () => media) }))
vi.mock('../../lib/endpoints', () => ({ fetchLibrary: vi.fn(), playFile: vi.fn() }))
const media = { id: 1, title: '作品', watched: 0, episodes: 12, genres: ['奇幻'], trailerId: 'dQw4w9WgXcQ' }
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')
const click = async (container: HTMLElement, selector: string) => act(async () => container.querySelector<HTMLButtonElement>(selector)!.click())

beforeEach(() => {
  vi.mocked(fetchLibrary).mockReset().mockResolvedValue({ clusters: [], folders: [], scannedAt: null, continueWatching: [] })
  vi.mocked(playFile).mockReset()
  vi.stubGlobal('localStorage', { getItem: () => null })
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function () { this.setAttribute('open', '') } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: function () {
    this.removeAttribute('open'); this.dispatchEvent(new Event('close', { bubbles: true }))
  } })
})
afterEach(() => {
  vi.unstubAllGlobals()
  for (const [name, descriptor] of [['showModal', show], ['close', close]] as const) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, name, descriptor)
    else Reflect.deleteProperty(HTMLDialogElement.prototype, name)
  }
})

describe('作品详情预览', () => {
  it('预告片按需加载，关闭后卸载播放器并恢复外层预览焦点', async () => {
    const change = vi.fn()
    const { container, unmount } = await mount(<MediaPreview media={media} onOpenChange={change}>预览</MediaPreview>)
    await click(container, 'button')
    expect(container.querySelector('iframe')).toBeNull()
    expect(fetchLibrary).not.toHaveBeenCalled()
    await click(container, 'button.media-preview-link')
    expect(container.querySelector('iframe')?.src).toContain('https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ')
    await click(container, '.media-trailer-dialog .media-preview-close')
    expect(container.querySelector('iframe')).toBeNull()
    expect(container.querySelector<HTMLDialogElement>('.media-preview')?.open).toBe(true)
    expect(document.activeElement).toBe(container.querySelector('button.media-preview-link'))
    expect(change.mock.calls).toEqual([[true]])
    await click(container, '.media-preview-body > .media-preview-close')
    expect(change.mock.calls).toEqual([[true], [false]])
    expect(document.activeElement).toBe(container.querySelector('button'))
    await unmount()
  })

  it('本地选集的关闭事件不能关闭外层预览，也不能自动播放演示条目', async () => {
    const { container, unmount } = await mount(<MediaPreview media={media}>预览</MediaPreview>)
    await click(container, 'button')
    await click(container, '.discover-card-play')
    expect(fetchLibrary).toHaveBeenCalledTimes(1)
    await click(container, '.media-play-close')
    expect(container.querySelector<HTMLDialogElement>('.media-preview')?.open).toBe(true)
    expect(playFile).not.toHaveBeenCalled()
    await unmount()
  })
})
