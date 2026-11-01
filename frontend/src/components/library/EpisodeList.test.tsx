// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import type { LibraryCluster, LibraryItem } from '../../lib/endpoints'
import type { MediaSummary } from '../media/types'
import { EpisodeList } from './EpisodeList'

const media: MediaSummary = { id: 1, title: '作品', episodes: 3, watched: 0, genres: [], status: 'RELEASING', episodeTitles: [
  { episode: 1, title: '第一集', airDate: '2020-01-01', image: '/first.jpg' },
  { episode: 2, title: '第二集', airDate: '2020-01-08', description: '第二集的简介' },
  { episode: 3, title: '尚未播出', airDate: '2099-01-01' },
] }

const item = (fileId: string, episode: number | null, extra: Partial<LibraryItem> = {}): LibraryItem => ({
  fileId, fileName: `${fileId}.mkv`, episode, kind: 'main', resolution: '1080p', sizeBytes: 1024, progress: null, ...extra,
})
const cluster = (groups: LibraryCluster['groups']): LibraryCluster => ({ clusterKey: 'k', title: '文件夹', season: 1, confidence: 1, episodeCount: 3, groups })
const group = (items: LibraryItem[], label = '', groupKey = 'main'): LibraryCluster['groups'][number] => ({ groupKey, label, sortMode: 'episode', items })

/** 能播的行：名字就是可见文字，以「播放」开头 */
const playable = (container: HTMLElement) => [...container.querySelectorAll<HTMLButtonElement>('li > button')].filter((b) => b.textContent?.startsWith('播放'))
/** 一行的文字（不管能不能播） */
const rows = (container: HTMLElement) => [...container.querySelectorAll('li')].map((li) => li.textContent ?? '')

const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
beforeEach(() => {
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function (this: HTMLDialogElement) { this.setAttribute('open', '') } })
})
afterEach(() => {
  if (show) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', show)
  else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal')
})

describe('作品页集数列表', () => {
  it('本地没有文件：已播出的集置灰列出，不能播；还没播的不列', async () => {
    const { container, unmount } = await mount(<EpisodeList catalog={media} pending={false} onPlay={vi.fn()} />)
    expect(rows(container)).toHaveLength(2)
    expect(rows(container)[0]).toContain('第一集')
    expect(rows(container)[0]).toContain('库里没有')
    expect(container.textContent).toContain('播出于 2020/01/01')
    expect(container.textContent).not.toContain('尚未播出')
    expect(playable(container)).toHaveLength(0)
    await unmount()
  })

  it('只有正片文件算入库：同编号的片头收进附加内容，那一集照样标「库里没有」', async () => {
    const local = cluster([group([item('ep1', 1), item('op2', 2, { kind: 'ncop' })])])
    const { container, unmount } = await mount(<EpisodeList cluster={local} catalog={media} pending={false} onPlay={vi.fn()} />)
    const names = playable(container).map((b) => b.textContent)
    expect(names[0]).toMatch(/^播放第 01 集：/)
    expect(rows(container).find((text) => text.includes('第二集'))).toContain('库里没有')
    expect(container.querySelector('details summary')?.textContent).toContain('附加内容')
    expect(names.some((name) => name?.startsWith('播放 op2.mkv：'))).toBe(true)
    await unmount()
  })

  it('本地状态叠在每一集上：已看完 / 看到百分之几 / 接下来 / 正在播放；点行就播那一集', async () => {
    const watched = item('ep1', 1, { progress: { positionSec: 100, durationSec: 100, completed: true } })
    const half = item('ep2', 2, { progress: { positionSec: 25, durationSec: 100, completed: false } })
    const fresh = item('ep3', 3)
    const onPlay = vi.fn()
    const { container, rerender, unmount } = await mount(<EpisodeList cluster={cluster([group([watched, half, fresh])])} next={fresh} pending={false} onPlay={onPlay} />)
    const [first, second, third] = playable(container)
    expect(first?.textContent).toContain('已看完')
    expect(second?.textContent).toContain('看到 25%')
    expect(third?.textContent).toContain('接下来')
    // 没有目录资料：标题是「第 N 集」，文件名另起一行
    expect(third?.textContent).toContain('ep3.mkv')
    await act(async () => second!.click())
    expect(onPlay).toHaveBeenCalledExactlyOnceWith(half)

    await rerender(<EpisodeList cluster={cluster([group([watched, half, fresh])])} next={fresh} pending={false} activeFileId="ep2" onPlay={onPlay} />)
    expect(playable(container)[1]?.textContent).toContain('正在播放')
    await unmount()
  })

  it('播放请求在途：每一行都禁用，防连点', async () => {
    const { container, unmount } = await mount(<EpisodeList cluster={cluster([group([item('ep1', 1)])])} pending onPlay={vi.fn()} />)
    expect(playable(container).every((b) => b.disabled)).toBe(true)
    await unmount()
  })

  it('同一集的其他版本挂在那一集下面，点了播那个版本；看完的标出来', async () => {
    const main = item('ep1', 1)
    const alt = item('ep1-bd', 1, { group: 'BD', progress: { positionSec: 1, durationSec: 1, completed: true } })
    const onPlay = vi.fn()
    const { container, unmount } = await mount(<EpisodeList cluster={cluster([group([main])])} versions={new Map([['ep1', [{ item: alt, label: 'BD · 1080p' }]]])} pending={false} onPlay={onPlay} />)
    const versions = container.querySelector('[role=group]')
    expect(versions?.getAttribute('aria-label')).toBe('第 01 集的其他版本')
    const button = versions!.querySelector('button')!
    expect(button.textContent).toBe('播放第 01 集的其他版本：BD · 1080p ✓（已看完）')
    expect(playable(container)[0]?.textContent).toContain('2 个版本')
    await act(async () => button.click())
    expect(onPlay).toHaveBeenCalledExactlyOnceWith(alt)
    await unmount()
  })

  it('子文件夹各成一节；库里没有的集说不准该插哪一节，单独成一节', async () => {
    const local = cluster([group([item('a1', 1)], 'Season 1', 's1'), group([item('b1', 1)], 'Season 2', 's2')])
    const { container, unmount } = await mount(<EpisodeList cluster={local} catalog={media} pending={false} onPlay={vi.fn()} />)
    const sections = [...container.querySelectorAll('section')].map((s) => s.getAttribute('aria-label'))
    expect(sections).toEqual(['Season 1', 'Season 2', '库里没有的集'])
    await unmount()
  })

  it('只有附加内容：先说明，再给「显示附加内容」', async () => {
    const { container, unmount } = await mount(<EpisodeList cluster={cluster([group([item('menu', null, { kind: 'menu' })])])} pending={false} onPlay={vi.fn()} />)
    expect(container.textContent).toContain('这里只有附加内容')
    expect(container.querySelector('details summary')?.textContent).toContain('显示附加内容')
    await unmount()
  })

  it('有本集简介的给「本集资料」，点开看简介；有浏览器播放地址的给链接', async () => {
    const local = cluster([group([item('ep2', 2, { stream: '/media/ep2' })])])
    const { container, unmount } = await mount(<EpisodeList cluster={local} catalog={media} pending={false} onPlay={vi.fn()} />)
    const link = [...container.querySelectorAll('a')].find((a) => a.textContent?.startsWith('在浏览器里播放'))
    expect(link?.getAttribute('href')).toBe('/watch/ep2')
    const details = [...container.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent === '本集资料：第 2 集')!
    await act(async () => details.click())
    const dialog = container.querySelector('dialog')!
    expect(dialog.open).toBe(true)
    expect(dialog.textContent).toContain('第二集的简介')
    await unmount()
  })
})
