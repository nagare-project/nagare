// @vitest-environment jsdom
import { act, useEffect, useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../test/harness'
import { EntryPage } from './EntryPage'
import { fetchLibrary } from '../lib/endpoints'
import type { LibraryCluster, PosterTones } from '../lib/endpoints'
import type { LocalPrimary } from '../components/media/MediaPlayButton'

const route = vi.hoisted(() => ({ id: 169580 }))
const shared = vi.hoisted(() => ({ play: vi.fn(), tones: null as PosterTones | null, torrentProps: [] as unknown[] }))
vi.mock('@tanstack/react-router', () => ({ useSearch: () => route, useRouter: () => null }))
vi.mock('../lib/endpoints', () => ({ fetchLibrary: vi.fn() }))
vi.mock('../components/media/useEpisodeMetadata', () => ({ useEpisodeMetadata: () => ({ episodes: [], retry: vi.fn() }) }))
const OFFLINE = 404
vi.mock('../components/media/useMediaDetails', () => ({ useMediaDetails: (id: number) => id === OFFLINE
  ? { error: '读取作品详情失败', retry: vi.fn() }
  : { media: { id, title: `作品 ${id}`, titleRomaji: `Sakuhin ${id}`, episodes: 12, watched: 0, genres: ['Drama'], format: 'TV', posterTones: shared.tones } } }))
vi.mock('../components/media/MediaPreview', () => ({ TrailerPreview: () => null }))
// 本地选集：有可播的集时把主按钮交给 hero（接口见 MediaPlayButton 的 onPrimaryChange）
vi.mock('../components/media/MediaPlayButton', () => ({ MediaPlayButton: ({ onPrimaryChange }: { onPrimaryChange?: (primary: LocalPrimary | null) => void }) => {
  useEffect(() => { onPrimaryChange?.({ label: '继续看第 3 集', busy: false, play: shared.play }) }, [])
  return <p>本地选集</p>
} }))
vi.mock('../components/media/MediaSourceButton', () => ({ MediaSourceButton: function Panel() { const [value, setValue] = useState(''); return <input aria-label="在线搜索词" value={value} onChange={e => setValue(e.target.value)} /> } }))
vi.mock('../components/media/MediaTorrentButton', () => ({ MediaTorrentButton: (props: { drawer?: boolean }) => { shared.torrentProps.push(props); return <button type="button">磁力</button> } }))

const TONES: PosterTones = { hue: 250, tone: '#a2b6e3', quiet: '#8597c6', mid: '#4f6290', low: '#141b2b', onTone: '#04070f' }
const tabs = (container: HTMLElement) => [...container.querySelectorAll<HTMLButtonElement>('[role="tab"]')]
const buttonNamed = (container: HTMLElement, text: string) => [...container.querySelectorAll<HTMLButtonElement>('button')].find(b => b.textContent === text)

beforeEach(() => {
  route.id = 169580
  shared.tones = null
  shared.play.mockReset()
  shared.torrentProps = []
  vi.stubGlobal('scrollTo', vi.fn())
})
afterEach(() => vi.unstubAllGlobals())

describe('目录作品页', () => {
  it('hero：标题、罗马音、动作行（海报色主按钮播本地下一集，磁力是抽屉入口）', async () => {
    const { container, unmount } = await mount(<EntryPage />)
    expect(container.querySelector('h1')?.textContent).toBe('作品 169580')
    expect(container.textContent).toContain('Sakuhin 169580')
    expect(document.title).toBe('作品 169580 · nagare')
    const primary = buttonNamed(container, '继续看第 3 集')!
    await act(async () => primary.click())
    expect(shared.play).toHaveBeenCalledOnce()
    expect(buttonNamed(container, '磁力')).toBeDefined()
    expect(shared.torrentProps.every(props => (props as { drawer?: boolean }).drawer === true)).toBe(true)
    await unmount()
  })

  it('海报色写在页面根元素上；没有色阶时不写，继承中性值', async () => {
    shared.tones = TONES
    const toned = await mount(<EntryPage />)
    const main = toned.container.querySelector<HTMLElement>('main')!
    expect(main.style.getPropertyValue('--poster-tone')).toBe('#a2b6e3')
    expect(main.style.getPropertyValue('--poster-on-tone')).toBe('#04070f')
    await toned.unmount()

    shared.tones = null
    const plain = await mount(<EntryPage />)
    expect(plain.container.querySelector<HTMLElement>('main')!.style.getPropertyValue('--poster-tone')).toBe('')
    await plain.unmount()
  })

  it('在线播放延迟加载，切换后保留输入，换作品时重置', async () => {
    const { container, rerender, unmount } = await mount(<EntryPage />)
    expect(tabs(container).map(tab => tab.textContent)).toEqual(['本地媒体库', '在线播放'])
    expect(container.querySelector('input')).toBeNull()
    await act(async () => tabs(container)[1]!.click())
    const input = container.querySelector('input')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '在线源搜索')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => tabs(container)[0]!.click())
    expect(input.closest('[hidden]')).not.toBeNull()
    await act(async () => tabs(container)[1]!.click())
    expect(container.querySelector('input')?.value).toBe('在线源搜索')
    expect(input.closest('[hidden]')).toBeNull()
    route.id = 2
    await rerender(<EntryPage />)
    expect(tabs(container)[0]?.getAttribute('aria-selected')).toBe('true')
    expect(container.querySelector('input')).toBeNull()
    expect(document.title).toBe('作品 2 · nagare')
    await unmount()
  })

  it('作品详情读不出来（离线）时，给出媒体库里这部作品的本地文件列表', async () => {
    const local: LibraryCluster = { clusterKey: 'k1', title: '本地文件夹', season: null, confidence: 0.9, episodeCount: 3, groups: [], matched: { anilistId: OFFLINE } }
    const other: LibraryCluster = { ...local, clusterKey: 'k2', title: '别的番', matched: { anilistId: 1 } }
    vi.mocked(fetchLibrary).mockResolvedValue({ folders: [], clusters: [local, other], continueWatching: [], scannedAt: 1 })
    route.id = OFFLINE
    const { container, unmount } = await mount(<EntryPage />)
    expect(container.querySelector('[role=alert]')?.textContent).toContain('读取作品详情失败')
    const links = [...container.querySelectorAll('a')].filter(a => a.getAttribute('href')?.startsWith('/anime/'))
    expect(links.map(a => a.getAttribute('href'))).toEqual(['/anime/k1'])
    await unmount()
  })
})
