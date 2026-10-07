// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import type { DirListing } from '../../lib/endpoints'
import { FolderBrowser } from './FolderBrowser'

// 每次 browseDirs 调用都挂起，由测试决定何时、以什么结果返回（验证过期响应与加载中的行为）
const pending: { path: string; signal?: AbortSignal; resolve: (listing: DirListing) => void }[] = []
vi.mock('../../lib/endpoints', () => ({
  browseDirs: vi.fn(
    (path: string, signal?: AbortSignal) => new Promise<DirListing>((resolve) => pending.push({ path, signal, resolve })),
  ),
}))

function listing(path: string, names: string[]): DirListing {
  return { path, parent: path === '' ? '' : '/', dirs: names.map((name) => ({ name, path: `${path}/${name}` })), truncated: false }
}

async function answer(index: number, value: DirListing): Promise<void> {
  await act(async () => {
    pending[index]!.resolve(value)
  })
}

function itemButton(container: HTMLElement, name: string): HTMLButtonElement {
  const button = [...container.querySelectorAll<HTMLButtonElement>('.folder-browser-item')].find((b) => b.textContent?.includes(name))
  if (button === undefined) throw new Error(`找不到文件夹：${name}`)
  return button
}

afterEach(() => {
  pending.length = 0
})

describe('FolderBrowser', () => {
  it('读完一层就把焦点放进列表，不把键盘用户甩回页面顶部', async () => {
    const { container, unmount } = await mount(<FolderBrowser onChoose={vi.fn()} onCancel={vi.fn()} />)
    await answer(0, listing('', ['主目录']))
    expect(document.activeElement).toBe(itemButton(container, '主目录'))

    await act(async () => itemButton(container, '主目录').click())
    // 读取中旧列表留在原地：被聚焦的元素没有被换掉
    expect(container.querySelector('.folder-browser-list')?.getAttribute('aria-busy')).toBe('true')
    expect(document.activeElement).toBe(itemButton(container, '主目录'))

    await answer(1, listing('/Users/you', ['Anime']))
    expect(document.activeElement).toBe(itemButton(container, 'Anime'))
    await unmount()
  })

  it('读取进行中再点不会再发请求（防双击连进两层）', async () => {
    const { container, unmount } = await mount(<FolderBrowser onChoose={vi.fn()} onCancel={vi.fn()} />)
    await answer(0, listing('', ['主目录', '下载']))

    await act(async () => itemButton(container, '主目录').click())
    await act(async () => itemButton(container, '下载').click())

    expect(pending.map((p) => p.path)).toEqual(['', '/主目录'])
    await unmount()
  })

  it('面板关掉时还在路上的请求被取消，晚到的响应不再落地', async () => {
    const { container, unmount } = await mount(<FolderBrowser onChoose={vi.fn()} onCancel={vi.fn()} />)
    await answer(0, listing('', ['主目录']))
    await act(async () => itemButton(container, '主目录').click())

    await unmount()

    expect(pending[1]!.signal?.aborted).toBe(true)
    await answer(1, listing('/主目录', ['晚到的结果']))
    expect(container.textContent).not.toContain('晚到的结果')
  })

  it('Esc 关闭；「添加这个文件夹」交出当前目录，在常用位置时不可点', async () => {
    const onChoose = vi.fn()
    const onCancel = vi.fn()
    const { container, unmount } = await mount(<FolderBrowser onChoose={onChoose} onCancel={onCancel} />)
    await answer(0, listing('', ['主目录']))
    const add = [...container.querySelectorAll('button')].find((b) => b.textContent === '添加这个文件夹')!
    expect(add.disabled).toBe(true)

    await act(async () => itemButton(container, '主目录').click())
    await answer(1, listing('/Users/you', []))
    expect(add.disabled).toBe(false)
    // 没有子文件夹时焦点落在「添加」上
    expect(document.activeElement).toBe(add)
    await act(async () => add.click())
    expect(onChoose).toHaveBeenCalledWith('/Users/you')

    await act(async () => {
      add.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(onCancel).toHaveBeenCalled()
    await unmount()
  })
})
