// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { fetchDownloads, removeDownload } from '../../lib/endpoints'
import type { DownloadTask } from '../../lib/endpoints'
import { DownloadsPanel } from './DownloadsPanel'

vi.mock('../../lib/endpoints', async original => ({
  ...await original<typeof import('../../lib/endpoints')>(),
  fetchDownloads: vi.fn(), removeDownload: vi.fn(),
}))

const task = (extra: Partial<DownloadTask>): DownloadTask => ({
  id: 'a'.repeat(40), title: '[LoliHouse] 葬送的芙莉莲 [01-28 合集]', root: '/dl', state: 'downloading',
  addedAt: 1, bytesDone: 0, peers: 0, seeders: 0, downRate: 0, ...extra,
})

beforeEach(() => {
  vi.useFakeTimers()
  vi.mocked(fetchDownloads).mockReset()
  vi.mocked(removeDownload).mockReset().mockResolvedValue(undefined)
})
afterEach(() => {
  vi.useRealTimers()
})

const button = (container: HTMLElement, text: string) => [...container.querySelectorAll<HTMLButtonElement>('button')].find(item => item.textContent === text)

describe('磁力任务页的下载列表', () => {
  it('列出在下的、下完的、失败的：进度、速度、落点、原因都看得见', async () => {
    vi.mocked(fetchDownloads).mockResolvedValue({ dir: '/Users/you/Downloads/nagare', downloads: [
      task({ name: '葬送的芙莉莲', size: 4 * 2 ** 30, bytesDone: 2 ** 30, downRate: 2 * 2 ** 20, peers: 12 }),
      task({ id: 'b'.repeat(40), state: 'metadata', title: '[老番] 某作品 01-26' }),
      task({ id: 'c'.repeat(40), state: 'done', name: '剧场版', size: 3 * 2 ** 30, bytesDone: 3 * 2 ** 30, paths: ['/Users/you/Downloads/nagare/剧场版.mkv'] }),
      task({ id: 'd'.repeat(40), state: 'failed', error: '这是私有站（PT）的种子，nagare 不下载' }),
    ] })
    const { container, unmount } = await mount(<DownloadsPanel />)
    const items = [...container.querySelectorAll('.download-item')]
    expect(container.textContent).toContain('下载目录：/Users/you/Downloads/nagare')
    expect(items).toHaveLength(4)
    expect(items[0]?.textContent).toContain('下载中 25%')
    expect(items[0]?.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow')).toBe('25')
    expect(items[0]?.textContent).toContain('分享者 12')
    expect(items[1]?.textContent).toContain('正在找分享者')
    expect(items[1]?.textContent).toContain('还没连上分享这个种子的人')
    expect(items[2]?.textContent).toContain('已完成')
    expect(items[2]?.textContent).toContain('/Users/you/Downloads/nagare/剧场版.mkv')
    expect(items[2]?.querySelector('a[href="/"]')).not.toBeNull()
    expect(items[3]?.textContent).toContain('私有站')
    await unmount()
  })

  it('没有下载时说明怎么下；取消正在下的那条会删掉已下载部分并刷新列表', async () => {
    vi.mocked(fetchDownloads).mockResolvedValueOnce({ dir: '/dl', downloads: [] })
    const empty = await mount(<DownloadsPanel />)
    expect(empty.container.textContent).toContain('还没有下载')
    await empty.unmount()

    vi.mocked(fetchDownloads).mockResolvedValueOnce({ dir: '/dl', downloads: [task({ size: 10, bytesDone: 5 })] })
      .mockResolvedValue({ dir: '/dl', downloads: [] })
    const { container, unmount } = await mount(<DownloadsPanel />)
    await act(async () => button(container, '取消并删除已下载部分')!.click())
    expect(removeDownload).toHaveBeenCalledExactlyOnceWith('a'.repeat(40))
    expect(container.querySelector('.download-item')).toBeNull()
    await unmount()
  })

  it('有在下的任务时 2 秒刷新一次', async () => {
    vi.mocked(fetchDownloads).mockResolvedValue({ dir: '/dl', downloads: [task({})] })
    const { unmount } = await mount(<DownloadsPanel />)
    expect(fetchDownloads).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(fetchDownloads).toHaveBeenCalledTimes(2)
    await unmount()
  })
})
