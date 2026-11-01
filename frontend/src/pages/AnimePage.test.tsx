// @vitest-environment jsdom
import { act } from 'react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../test/harness'
import type { LibraryCluster, LibraryData } from '../lib/endpoints'
import { AnimePage } from './AnimePage'

const shared = vi.hoisted(() => ({ clusterKey: 'k1', data: null as LibraryData | null, apply: vi.fn(), playFile: vi.fn(), picks: [] as number[] }))
vi.mock('@tanstack/react-router', () => ({
  useParams: () => ({ clusterKey: shared.clusterKey }),
  useRouter: () => null,
  Link: ({ to, children, className }: { to: string; children: ReactNode; className?: string }) => <a href={to} className={className}>{children}</a>,
}))
vi.mock('../hooks/useLibrary', () => ({ useLibrary: () => ({ state: { phase: 'ready', data: shared.data }, reload: vi.fn() }) }))
vi.mock('../hooks/usePlayerStatus', () => ({ usePlayerStatus: () => ({ status: null, apply: shared.apply }) }))
vi.mock('../lib/endpoints', () => ({ playFile: shared.playFile }))
// 对应作品面板自己有测试；这里只看 hero 的「认作品」有没有把请求递过去
vi.mock('../components/library/AssociationPanel', () => ({ AssociationPanel: ({ pickRequest }: { pickRequest?: number }) => {
  shared.picks.push(pickRequest ?? 0)
  return <section aria-label="对应作品" />
} }))

const files = (key: string) => [
  { fileId: `${key}-1`, fileName: `${key} - 01.mkv`, episode: 1, kind: 'main', resolution: '1080p', sizeBytes: 1, progress: { positionSec: 10, durationSec: 10, completed: true } },
  { fileId: `${key}-2`, fileName: `${key} - 02.mkv`, episode: 2, kind: 'main', resolution: '1080p', sizeBytes: 1, progress: null },
]
const make = (patch: Partial<LibraryCluster>): LibraryCluster => ({
  clusterKey: 'k1', title: 'Some Folder', season: 1, confidence: 0.9, episodeCount: 2,
  groups: [{ groupKey: 'main', label: '正片', sortMode: 'episode', items: files('k1') }], ...patch,
})
const library = (...clusters: LibraryCluster[]): LibraryData => ({ folders: [], clusters, continueWatching: [], scannedAt: 1 })
const buttonNamed = (container: HTMLElement, text: string) => [...container.querySelectorAll<HTMLButtonElement>('button')].find(b => b.textContent === text)

beforeEach(() => {
  shared.clusterKey = 'k1'
  shared.picks = []
  shared.playFile.mockReset().mockResolvedValue({ title: '第 2 集', danmaku: { state: 'none' } })
  shared.apply.mockReset()
})
afterEach(() => { shared.data = null })

describe('本地分组页', () => {
  it('没认出作品：没有海报，标题取自文件夹，主按钮是「认作品」，打开对应作品面板的选作品对话框', async () => {
    shared.data = library(make({}))
    const { container, unmount } = await mount(<AnimePage />)
    expect(container.querySelector('h1')?.textContent).toBe('Some Folder')
    expect(container.querySelector('.work-hero-poster')).toBeNull()
    const pick = buttonNamed(container, '认作品')!
    expect(pick.className).toContain('hero-btn--solid')
    await act(async () => pick.click())
    expect(shared.picks.at(-1)).toBe(1)
    // 播放还在，降成玻璃按钮：一页只有一个实心
    expect(buttonNamed(container, '播放第 2 集')?.className).toContain('hero-btn--heavy')
    await unmount()
  })

  it('认出作品：作品名做标题、海报色写在页面根上，主按钮播下一集，「作品详情」链到目录作品页', async () => {
    shared.data = library(make({
      association: { mode: 'manual', anilistId: 42, title: '认定的作品', setAt: 1 },
      cover: '/art/k1',
      posterTones: { hue: 30, tone: '#e0b49a', quiet: '#c29a82', mid: '#8a6450', low: '#2b1d15', onTone: '#120a06' },
    }))
    const { container, unmount } = await mount(<AnimePage />)
    expect(container.querySelector('h1')?.textContent).toBe('认定的作品')
    expect(container.textContent).toContain('文件夹：Some Folder')
    expect(container.querySelector<HTMLElement>('main')!.style.getPropertyValue('--poster-tone')).toBe('#e0b49a')
    expect(buttonNamed(container, '认作品')).toBeUndefined()
    expect([...container.querySelectorAll('a')].find(a => a.textContent === '作品详情')?.getAttribute('href')).toBe('/entry?id=42')
    await act(async () => buttonNamed(container, '播放第 2 集')!.click())
    expect(shared.playFile).toHaveBeenCalledExactlyOnceWith('k1-2')
    expect(shared.apply).toHaveBeenCalledWith(expect.objectContaining({ playing: true, fileId: 'k1-2' }))
    await unmount()
  })

  it('集数列表里点一集就播那一集；播放失败把原因说出来', async () => {
    shared.data = library(make({}))
    shared.playFile.mockRejectedValueOnce(new Error('未找到 mpv'))
    const { container, unmount } = await mount(<AnimePage />)
    const row = [...container.querySelectorAll<HTMLButtonElement>('li > button')].find(b => b.textContent?.startsWith('播放第 01 集：'))!
    await act(async () => row.click())
    expect(shared.playFile).toHaveBeenCalledExactlyOnceWith('k1-1')
    expect(container.querySelector('[role=status]')?.textContent).toBe('未找到 mpv')
    await unmount()
  })

  it('分组键失效（重新扫描后归到了别的分组）：给一条回媒体库的路', async () => {
    shared.data = library(make({}))
    shared.clusterKey = 'gone'
    const { container, unmount } = await mount(<AnimePage />)
    expect(container.textContent).toContain('找不到这部作品')
    expect(container.querySelector('a')?.getAttribute('href')).toBe('/library')
    await unmount()
  })
})
