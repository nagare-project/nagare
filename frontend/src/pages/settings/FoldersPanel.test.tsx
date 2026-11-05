// @vitest-environment jsdom
import { act, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { UseLibraryResult } from '../../hooks/useLibrary'
import type { LibraryData } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { getAllByRole, getByRole, getByText, queryByRole } from '../../test/queries'
import { FoldersPanel } from './FoldersPanel'

const FOLDERS: LibraryData['folders'] = [
  { id: 'a', path: '/Volumes/Anime', addedAt: 1_756_500_000 },
  { id: 'b', path: '/Users/you/Movies', addedAt: 1_756_600_000, error: '没有读取权限' },
]

/** 照 useLibrary 的样子：删除成功时列表跟着少一行，失败时原样抛出 */
function Harness({ fail = false }: { fail?: boolean }) {
  const [folders, setFolders] = useState(FOLDERS)
  const library: UseLibraryResult = {
    state: { phase: 'ready', data: { folders, clusters: [], continueWatching: [], scannedAt: 1_756_600_000 } },
    reload: async () => {},
    addFolder: async () => { throw new Error('不在这里测') },
    removeFolder: async (id) => {
      if (fail) throw new Error('文件夹正在扫描，稍后再删')
      setFolders((list) => list.filter((folder) => folder.id !== id))
    },
    rescan: async () => { throw new Error('不在这里测') },
  }
  return <FoldersPanel library={library} platform="darwin" />
}

const mounted: Array<() => Promise<void>> = []
afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.restoreAllMocks()
})

describe('FoldersPanel', () => {
  it('每个文件夹一行：路径、扫描出错的原因，「删除」的名字里带着是哪个文件夹', async () => {
    const m = await mount(<Harness />)
    mounted.push(m.unmount)
    const list = getByRole(getByRole(m.container, 'region', { name: '已添加的文件夹' }), 'list')
    expect(getAllByRole(list, 'listitem')).toHaveLength(2)
    expect(getByRole(m.container, 'alert').textContent).toBe('扫描出错：没有读取权限')
    expect(getByRole(m.container, 'button', { name: '删除文件夹 /Volumes/Anime' })).toBeDefined()
  })

  it('删掉一行后焦点交给列表标题，不掉到 body 上', async () => {
    const m = await mount(<Harness />)
    mounted.push(m.unmount)
    const remove = getByRole(m.container, 'button', { name: '删除文件夹 /Volumes/Anime' })
    remove.focus()
    await act(async () => { remove.click() })

    expect(queryByRole(m.container, 'button', { name: '删除文件夹 /Volumes/Anime' })).toBeNull()
    expect(getAllByRole(m.container, 'listitem')).toHaveLength(1)
    expect(document.activeElement).toBe(getByRole(m.container, 'heading', { level: 3, name: '已添加的文件夹' }))
  })

  it('删除失败：原因写出来，那一行还在、能再点；焦点一直留在「删除」上（请求中它只是 aria-disabled）', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const m = await mount(<Harness fail />)
    mounted.push(m.unmount)
    const remove = getByRole(m.container, 'button', { name: '删除文件夹 /Volumes/Anime' }) as HTMLButtonElement
    remove.focus()
    await act(async () => { remove.click() })

    expect(getByText(m.container, '文件夹正在扫描，稍后再删')).toBeDefined()
    const again = getByRole(m.container, 'button', { name: '删除文件夹 /Volumes/Anime' }) as HTMLButtonElement
    expect(again).toBe(remove)
    expect(again.disabled).toBe(false)
    expect(again.hasAttribute('aria-disabled')).toBe(false)
    expect(document.activeElement).toBe(again)
  })

  it('删除中：各行的「删除」都不可用但仍可聚焦，再点不会再发一次', async () => {
    let finish: () => void = () => {}
    const removeFolder = vi.fn(() => new Promise<void>((resolve) => { finish = resolve }))
    const library: UseLibraryResult = {
      state: { phase: 'ready', data: { folders: FOLDERS, clusters: [], continueWatching: [], scannedAt: 1_756_600_000 } },
      reload: async () => {},
      addFolder: async () => { throw new Error('不在这里测') },
      removeFolder,
      rescan: async () => { throw new Error('不在这里测') },
    }
    const m = await mount(<FoldersPanel library={library} platform="darwin" />)
    mounted.push(m.unmount)
    const remove = getByRole(m.container, 'button', { name: '删除文件夹 /Volumes/Anime' })
    remove.focus()
    await act(async () => { remove.click() })
    const pending = getByRole(m.container, 'button', { name: '删除中 …文件夹 /Volumes/Anime' })
    expect(pending.getAttribute('aria-disabled')).toBe('true')
    expect(document.activeElement).toBe(pending)
    const other = getByRole(m.container, 'button', { name: '删除文件夹 /Users/you/Movies' })
    expect(other.getAttribute('aria-disabled')).toBe('true')
    await act(async () => { pending.click(); other.click() })
    expect(removeFolder).toHaveBeenCalledTimes(1)
    await act(async () => { finish() })
  })
})
