// @vitest-environment jsdom
import { act, useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../test/harness'
import { EntryPage } from './EntryPage'

const route = vi.hoisted(() => ({ id: 169580 }))
vi.mock('@tanstack/react-router', () => ({ useSearch: () => route }))
vi.mock('../components/media/useEpisodeMetadata', () => ({ useEpisodeMetadata: () => ({ episodes: [], retry: vi.fn() }) }))
vi.mock('../components/media/useMediaDetails', () => ({ useMediaDetails: (id: number) => ({ media: { id, title: `作品 ${id}`, episodes: 12, watched: 0, genres: [], format: 'TV' } }) }))
vi.mock('../components/media/MediaDetails', () => ({ MediaDetails: ({ media, children }: any) => <><h1>{media.title}</h1>{children}</>, MediaExternalLinks: () => null, MediaRelations: () => null }))
vi.mock('../components/media/MediaPreview', () => ({ TrailerPreview: () => null }))
vi.mock('../components/media/MediaPlayButton', () => ({ MediaPlayButton: () => <p>本地选集</p> }))
vi.mock('../components/media/MediaSourceButton', () => ({ MediaSourceButton: () => <p>在线选集</p> }))
vi.mock('../components/media/MediaTorrentButton', () => ({ MediaTorrentButton: function Panel() { const [value, setValue] = useState(''); return <input aria-label="搜索词" value={value} onChange={e => setValue(e.target.value)} /> } }))

beforeEach(() => { route.id = 169580; vi.stubGlobal('scrollTo', vi.fn()) })
afterEach(() => vi.unstubAllGlobals())

describe('作品详情来源导航', () => {
  it('延迟加载来源，切换后保留输入，换作品时重置', async () => {
    const { container, rerender, unmount } = await mount(<EntryPage />)
    const tabs = () => [...container.querySelectorAll<HTMLButtonElement>('[role="tab"]')]
    expect(tabs().map(tab => tab.textContent)).toEqual(['本地媒体库', '磁力播放', '在线播放'])
    expect(container.querySelector('input')).toBeNull()
    await act(async () => tabs()[1]!.click())
    const input = container.querySelector('input')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '字幕组选择')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => tabs()[2]!.click())
    expect(input.closest('[hidden]')).not.toBeNull()
    await act(async () => tabs()[1]!.click())
    expect(container.querySelector('input')?.value).toBe('字幕组选择')
    expect(input.closest('[hidden]')).toBeNull()
    route.id = 2
    await rerender(<EntryPage />)
    expect(tabs()[0]?.getAttribute('aria-selected')).toBe('true')
    expect(container.querySelector('input')).toBeNull()
    expect(document.title).toBe('作品 2 · nagare')
    await unmount()
  })
})
