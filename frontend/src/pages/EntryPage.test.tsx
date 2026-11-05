// @vitest-environment jsdom
import { act, useEffect, useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../test/harness'
import { getAllByRole, getByRole, getByText, queryByRole } from '../test/queries'
import { installLocalStorage } from '../test/storage'
import { LOGIN_HINT_DISMISS_KEY } from '../components/library/WorkStates'
import { EntryPage } from './EntryPage'
import { fetchLibrary } from '../lib/endpoints'
import type { LibraryCluster, PosterTones } from '../lib/endpoints'
import type { LocalPrimary } from '../components/media/MediaPlayButton'

const route = vi.hoisted(() => ({ id: 169580 }))
const shared = vi.hoisted(() => ({ play: vi.fn(), tones: null as PosterTones | null, torrentProps: [] as unknown[], detail: {} as Record<string, unknown>, loggedIn: true }))
vi.mock('@tanstack/react-router', () => ({ useSearch: () => route, useRouter: () => null }))
vi.mock('../lib/endpoints', () => ({ fetchLibrary: vi.fn() }))
vi.mock('../components/media/useEpisodeMetadata', () => ({ useEpisodeMetadata: () => ({ episodes: [], retry: vi.fn() }) }))
const OFFLINE = 404
const LOADING = 405
vi.mock('../components/media/useMediaDetails', () => ({ useMediaDetails: (id: number) => id === OFFLINE
  ? { error: '读取作品详情失败', retry: vi.fn() }
  : id === LOADING
    ? { loading: true, retry: vi.fn() }
    : { media: { id, title: `作品 ${id}`, titleRomaji: `Sakuhin ${id}`, episodes: 12, watched: 0, genres: ['Drama'], format: 'TV', posterTones: shared.tones, ...shared.detail } } }))
// 账号状态（没登录时作品页说一句「登录后显示弹幕、同步进度」）
vi.mock('../components/media/CollectionContext', () => ({ useCollection: () => ({ loggedIn: shared.loggedIn, entries: [], loading: false, error: null }) }))
vi.mock('../components/media/MediaPreview', () => ({ TrailerPreview: () => null, MediaPreview: () => null }))
// 本地选集：有可播的集时把主按钮交给 hero（接口见 MediaPlayButton 的 onPrimaryChange）
vi.mock('../components/media/MediaPlayButton', () => ({ MediaPlayButton: ({ onPrimaryChange }: { onPrimaryChange?: (primary: LocalPrimary | null) => void }) => {
  useEffect(() => { onPrimaryChange?.({ label: '继续看第 3 集', busy: false, play: shared.play }) }, [])
  return <p>本地选集</p>
} }))
vi.mock('../components/media/MediaSourceButton', () => ({ MediaSourceButton: function Panel() { const [value, setValue] = useState(''); return <input aria-label="在线搜索词" value={value} onChange={e => setValue(e.target.value)} /> } }))
vi.mock('../components/media/MediaTorrentButton', () => ({ MediaTorrentButton: (props: { drawer?: boolean }) => { shared.torrentProps.push(props); return <button type="button">磁力</button> } }))

const TONES: PosterTones = { hue: 250, tone: '#a2b6e3', quiet: '#8597c6', mid: '#4f6290', low: '#141b2b', onTone: '#04070f' }
const tabs = (container: HTMLElement) => getAllByRole(container, 'tab') as HTMLButtonElement[]

beforeEach(() => {
  route.id = 169580
  shared.tones = null
  shared.detail = {}
  shared.loggedIn = true
  shared.play.mockReset()
  shared.torrentProps = []
  vi.stubGlobal('scrollTo', vi.fn())
  installLocalStorage()
})
afterEach(() => vi.unstubAllGlobals())

describe('目录作品页', () => {
  it('hero：标题、罗马音、动作行（海报色主按钮播本地下一集，磁力是抽屉入口）', async () => {
    const { container, unmount } = await mount(<EntryPage />)
    expect(getByRole(container, 'heading', { level: 1 }).textContent).toBe('作品 169580')
    expect(getByText(container, 'Sakuhin 169580').getAttribute('lang')).toBe('ja-Latn')
    expect(document.title).toBe('作品 169580 · nagare')
    const primary = getByRole(container, 'button', { name: '继续看第 3 集' })
    expect(primary.className).toContain('hero-btn--solid')
    await act(async () => primary.click())
    expect(shared.play).toHaveBeenCalledOnce()
    expect(getByRole(container, 'button', { name: '磁力' })).toBeDefined()
    expect(shared.torrentProps.every(props => (props as { drawer?: boolean }).drawer === true)).toBe(true)
    await unmount()
  })

  it('海报色写在页面根元素上；没有色阶时不写，继承中性值', async () => {
    shared.tones = TONES
    const toned = await mount(<EntryPage />)
    const main = getByRole(toned.container, 'main')
    expect(main.style.getPropertyValue('--poster-tone')).toBe('#a2b6e3')
    expect(main.style.getPropertyValue('--poster-on-tone')).toBe('#04070f')
    await toned.unmount()

    shared.tones = null
    const plain = await mount(<EntryPage />)
    expect(getByRole(plain.container, 'main').style.getPropertyValue('--poster-tone')).toBe('')
    await plain.unmount()
  })

  it('在线播放延迟加载，切换后保留输入，换作品时重置', async () => {
    const { container, rerender, unmount } = await mount(<EntryPage />)
    expect(tabs(container).map(tab => tab.textContent)).toEqual(['本地媒体库', '在线播放'])
    expect(getByRole(container, 'tablist', { name: '播放来源' })).toBeDefined()
    expect(queryByRole(container, 'textbox', { name: '在线搜索词', hidden: true })).toBeNull()
    await act(async () => tabs(container)[1]!.click())
    expect(getByRole(container, 'tabpanel', { name: '在线播放' })).toBeDefined()
    const input = getByRole(container, 'textbox', { name: '在线搜索词' }) as HTMLInputElement
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '在线源搜索')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => tabs(container)[0]!.click())
    // 切走只是藏起来：读屏碰不到，但输入还在
    expect(queryByRole(container, 'textbox', { name: '在线搜索词' })).toBeNull()
    await act(async () => tabs(container)[1]!.click())
    expect((getByRole(container, 'textbox', { name: '在线搜索词' }) as HTMLInputElement).value).toBe('在线源搜索')
    route.id = 2
    await rerender(<EntryPage />)
    expect(tabs(container)[0]?.getAttribute('aria-selected')).toBe('true')
    expect(queryByRole(container, 'textbox', { name: '在线搜索词', hidden: true })).toBeNull()
    expect(document.title).toBe('作品 2 · nagare')
    await unmount()
  })

  it('作品详情读不出来（离线）时，给出媒体库里这部作品的本地文件列表', async () => {
    const local: LibraryCluster = { clusterKey: 'k1', title: '本地文件夹', season: null, confidence: 0.9, episodeCount: 3, groups: [], matched: { anilistId: OFFLINE } }
    const other: LibraryCluster = { ...local, clusterKey: 'k2', title: '别的番', matched: { anilistId: 1 } }
    vi.mocked(fetchLibrary).mockResolvedValue({ folders: [], clusters: [local, other], continueWatching: [], scannedAt: 1 })
    route.id = OFFLINE
    const { container, unmount } = await mount(<EntryPage />)
    expect(getByRole(container, 'alert').textContent).toContain('读取作品详情失败')
    expect(getByRole(container, 'link', { name: '返回发现' }).getAttribute('href')).toBe('/')
    const localLink = getByRole(container, 'link', { name: '本地文件夹（3 集）' })
    expect(localLink.getAttribute('href')).toBe('/anime/k1')
    expect(getAllByRole(container, 'link').filter(a => a.getAttribute('href')?.startsWith('/anime/'))).toEqual([localLink])
    await unmount()
  })

  it('读取中是 hero 的骨架（读屏念「正在加载作品详情」）', async () => {
    route.id = LOADING
    const { container, unmount } = await mount(<EntryPage />)
    expect(getByRole(container, 'status', { name: '正在加载作品详情' })).toBeDefined()
    expect(queryByRole(container, 'heading', { level: 1 })).toBeNull()
    await unmount()
  })

  it('离线时 hero 里说清作品资料是什么时候的；在线拿到的新资料不说', async () => {
    shared.detail = { freshness: { fetchedAt: new Date(Date.now() - 3 * 3600_000).toISOString(), stale: false, offline: true } }
    const offline = await mount(<EntryPage />)
    expect(getByRole(offline.container, 'status').textContent).toBe('离线 · 更新于 3 小时前')
    await offline.unmount()
    shared.detail = {}
    const online = await mount(<EntryPage />)
    expect(queryByRole(online.container, 'status')).toBeNull()
    await online.unmount()
  })

  it('没登录时说一次「登录后显示弹幕、同步进度」，关掉后不再出现；登录了不说', async () => {
    shared.loggedIn = false
    const first = await mount(<EntryPage />)
    expect(getByText(first.container, /^登录后显示弹幕、同步进度/)).toBeDefined()
    expect(getByRole(first.container, 'link', { name: '去登录' }).getAttribute('href')).toBe('/settings#account')
    await act(async () => getByRole(first.container, 'button', { name: '关闭这条说明' }).click())
    expect(first.container.textContent).not.toContain('登录后显示弹幕')
    expect(window.localStorage.getItem(LOGIN_HINT_DISMISS_KEY)).toBe('1')
    await first.unmount()
    const second = await mount(<EntryPage />)
    expect(second.container.textContent).not.toContain('登录后显示弹幕')
    await second.unmount()

    installLocalStorage()
    shared.loggedIn = true
    const loggedIn = await mount(<EntryPage />)
    expect(loggedIn.container.textContent).not.toContain('登录后显示弹幕')
    await loggedIn.unmount()
  })

  it('关联作品与推荐用发现页同一种作品卡：链到作品页，关联类型念得出来', async () => {
    shared.detail = {
      relations: [{ type: 'SEQUEL', media: { id: 7, title: '续作', episodes: 12, watched: 0, genres: [] } }],
      recommendations: [{ id: 8, title: '推荐作品', episodes: 24, watched: 0, genres: [], year: 2026, season: 'FALL' }],
    }
    const { container, unmount } = await mount(<EntryPage />)
    const relations = getByRole(container, 'region', { name: '关联作品' })
    expect(getByRole(relations, 'link', { name: /续作/ }).getAttribute('href')).toBe('/entry?id=7')
    const recommendations = getByRole(container, 'region', { name: '相关推荐' })
    expect(getByRole(recommendations, 'link', { name: /^推荐作品/ }).getAttribute('href')).toBe('/entry?id=8')
    await unmount()
  })
})
