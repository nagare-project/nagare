// @vitest-environment jsdom
import { act } from 'react'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import type { LibraryCluster } from '../../lib/endpoints'
import type { MediaSummary } from '../media/types'
import { AssociationPanel } from './AssociationPanel'

const api = vi.hoisted(() => ({
  setAssociation: vi.fn(),
  dismissSyncedElsewhere: vi.fn(),
  fetchCatalogSearch: vi.fn(),
}))
vi.mock('../../lib/associations', async (original) => ({
  ...(await original<typeof import('../../lib/associations')>()),
  setAssociation: api.setAssociation,
  dismissSyncedElsewhere: api.dismissSyncedElsewhere,
}))
vi.mock('../../lib/media', () => ({ fetchCatalogSearch: api.fetchCatalogSearch }))

// jsdom 没有 <dialog> 的模态方法
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')
beforeAll(() => {
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function (this: HTMLDialogElement) { this.setAttribute('open', '') } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: function (this: HTMLDialogElement) {
    if (!this.hasAttribute('open')) return
    this.removeAttribute('open'); this.dispatchEvent(new Event('close'))
  } })
})
afterAll(() => {
  if (show) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', show)
  else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal')
  if (close) Object.defineProperty(HTMLDialogElement.prototype, 'close', close)
  else Reflect.deleteProperty(HTMLDialogElement.prototype, 'close')
})
afterEach(() => {
  // reset 而不是 clear：一个用例里设的 mockRejectedValue 不能漏进下一个
  api.setAssociation.mockReset()
  api.dismissSyncedElsewhere.mockReset()
  api.fetchCatalogSearch.mockReset()
})

function cluster(patch: Partial<LibraryCluster> = {}): LibraryCluster {
  return { clusterKey: 'k1', title: 'Sousou no Frieren', season: null, confidence: 0.9, episodeCount: 2, groups: [], ...patch }
}

function button(container: HTMLElement, text: string): HTMLButtonElement {
  const found = [...container.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.includes(text))
  if (!found) throw new Error(`找不到按钮：${text}`)
  return found
}

async function settle(): Promise<void> {
  for (let i = 0; i < 4; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
}

describe('AssociationPanel', () => {
  it('自动匹配到的作品：链到作品页，一键确认后让页面重读媒体库', async () => {
    api.setAssociation.mockResolvedValue({ mode: 'manual', anilistId: 154587, setAt: 1 })
    const onChanged = vi.fn()
    const { container, unmount } = await mount(<AssociationPanel cluster={cluster({ matched: { anilistId: 154587, title: '葬送的芙莉莲' } })} onChanged={onChanged} />)

    expect(container.textContent).toContain('自动匹配到「葬送的芙莉莲」，还没确认')
    expect(container.querySelector('a')?.getAttribute('href')).toBe('/entry?id=154587')

    await act(async () => button(container, '确认').click())

    expect(api.setAssociation).toHaveBeenCalledWith('k1', { mode: 'manual', anilistId: 154587 })
    expect(onChanged).toHaveBeenCalled()
    await unmount()
  })

  it('归组置信度低时提醒标题可能没认准；什么都没匹配到时给「选择作品」', async () => {
    const low = await mount(<AssociationPanel cluster={cluster({ confidence: 0.4, matched: { anilistId: 1, title: '某番' } })} onChanged={vi.fn()} />)
    expect(low.container.textContent).toContain('标题可能没认准')
    await low.unmount()

    const empty = await mount(<AssociationPanel cluster={cluster()} onChanged={vi.fn()} />)
    expect(empty.container.textContent).toContain('还没有对应的作品')
    expect(button(empty.container, '选择作品')).toBeTruthy()
    expect([...empty.container.querySelectorAll('button')].some((b) => b.textContent === '确认')).toBe(false)
    await empty.unmount()
  })

  it('之前写到别的作品上的集：说清是哪几集、链到那部作品；处理完可以去掉', async () => {
    api.dismissSyncedElsewhere.mockResolvedValue({ mode: 'manual', anilistId: 1, setAt: 1 })
    const onChanged = vi.fn()
    const c = cluster({ association: { mode: 'manual', anilistId: 1, title: '对的番', setAt: 1, syncedElsewhere: [{ anilistId: 999, title: '认错的番', episodes: [3, 1, 2] }] } })
    const { container, unmount } = await mount(<AssociationPanel cluster={c} onChanged={onChanged} />)

    expect(container.textContent).toContain('第 1–3 集之前记到了「认错的番」上')
    const fix = [...container.querySelectorAll('a')].find((a) => a.textContent?.includes('去「认错的番」改进度'))
    expect(fix?.getAttribute('href')).toBe('/entry?id=999')

    await act(async () => button(container, '已处理').click())

    expect(api.dismissSyncedElsewhere).toHaveBeenCalledWith('k1', 999)
    expect(onChanged).toHaveBeenCalled()
    await unmount()
  })

  it('回到自动匹配会删掉那份清单：清单不空时先确认；连点两下、取消、Esc 都不删', async () => {
    api.setAssociation.mockResolvedValue(null)
    const c = cluster({ association: { mode: 'manual', anilistId: 1, title: '对的番', setAt: 1, syncedElsewhere: [{ anilistId: 999, episodes: [1] }] } })
    const { container, unmount } = await mount(<AssociationPanel cluster={c} onChanged={vi.fn()} />)

    const entry = button(container, '回到自动匹配')
    // 两次点击分开提交：第二下落在已经重新渲染过的按钮上
    await act(async () => entry.click())
    await act(async () => entry.click())
    expect(api.setAssociation).not.toHaveBeenCalled()
    expect(container.textContent).toContain('清单也会一起删掉')
    expect(entry.disabled).toBe(true)
    expect(document.activeElement).toBe(button(container, '取消'))

    await act(async () => button(container, '取消').click())
    expect(container.textContent).not.toContain('清单也会一起删掉')
    expect(document.activeElement, '取消后焦点回到入口，不掉到页面顶部').toBe(button(container, '回到自动匹配'))

    await act(async () => button(container, '回到自动匹配').click())
    await act(async () => {
      container.querySelector('.assoc-confirm')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(container.textContent).not.toContain('清单也会一起删掉')
    expect(api.setAssociation).not.toHaveBeenCalled()

    await act(async () => button(container, '回到自动匹配').click())
    await act(async () => button(container, '确定删除并回到自动匹配').click())
    expect(api.setAssociation).toHaveBeenCalledTimes(1)
    expect(api.setAssociation).toHaveBeenCalledWith('k1', { mode: 'auto' })
    await unmount()
  })

  it('保存成功后焦点放到状态行上（点过的按钮已经没了），保存与重读期间按钮全禁用', async () => {
    api.setAssociation.mockResolvedValue({ mode: 'manual', anilistId: 1, setAt: 1 })
    let finishReload = () => {}
    const onChanged = vi.fn(() => new Promise<void>((resolve) => { finishReload = resolve }))
    const { container, unmount } = await mount(<AssociationPanel cluster={cluster({ matched: { anilistId: 1, title: '某番' } })} onChanged={onChanged} />)

    await act(async () => button(container, '确认').click())
    // 还在重读媒体库：按旧数据画的按钮不能点
    expect([...container.querySelectorAll<HTMLButtonElement>('.assoc-panel-actions button')].every((b) => b.disabled)).toBe(true)
    expect(container.textContent).toContain('正在保存')

    await act(async () => finishReload())
    expect(document.activeElement).toBe(container.querySelector('.assoc-panel-status'))
    await unmount()
  })

  it('「已处理」失败时说出原因，焦点放到错误说明上', async () => {
    api.dismissSyncedElsewhere.mockRejectedValue(new Error('这个分组已经没有作品关联了'))
    const c = cluster({ association: { mode: 'none', setAt: 1, syncedElsewhere: [{ anilistId: 999, title: '认错的番', episodes: [1] }] } })
    const { container, unmount } = await mount(<AssociationPanel cluster={c} onChanged={vi.fn()} />)

    await act(async () => button(container, '已处理').click())

    const alert = container.querySelector<HTMLElement>('[role="alert"]')
    expect(alert?.textContent).toContain('已经没有作品关联')
    expect(document.activeElement).toBe(alert)
    await unmount()
  })

  it('没有清单时回到自动匹配不再多问；失败时说出原因、不让页面重读', async () => {
    api.setAssociation.mockRejectedValue(new Error('正在播放这部作品，停止播放后再改'))
    const onChanged = vi.fn()
    const { container, unmount } = await mount(<AssociationPanel cluster={cluster({ association: { mode: 'none', setAt: 1 } })} onChanged={onChanged} />)
    expect(container.textContent).toContain('已标为不是目录里的作品')

    await act(async () => button(container, '回到自动匹配').click())

    expect(api.setAssociation).toHaveBeenCalledWith('k1', { mode: 'auto' })
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('正在播放这部作品')
    expect(onChanged).not.toHaveBeenCalled()
    await unmount()
  })
})

describe('AssociationDialog（经面板打开）', () => {
  const frieren: MediaSummary = { id: 154587, title: '葬送的芙莉莲', titleNative: '葬送のフリーレン', episodes: 28, watched: 0, genres: [], format: 'TV', year: 2023 } as MediaSummary
  const other: MediaSummary = { id: 2, title: '别的番', episodes: 12, watched: 0, genres: [] } as MediaSummary

  it('打开时用文件夹标题搜一次；选中结果就认定为那部作品', async () => {
    api.fetchCatalogSearch.mockResolvedValue([frieren, other])
    api.setAssociation.mockResolvedValue({ mode: 'manual', anilistId: 154587, setAt: 1 })
    const onChanged = vi.fn()
    const { container, unmount } = await mount(<AssociationPanel cluster={cluster({ matched: { anilistId: 2, title: '别的番' } })} onChanged={onChanged} />)

    await act(async () => button(container, '更换').click())
    await settle()

    expect(container.querySelector('dialog')?.hasAttribute('open')).toBe(true)
    expect(api.fetchCatalogSearch).toHaveBeenCalledWith('Sousou no Frieren', expect.any(AbortSignal))
    expect(container.textContent).toContain('2023 · TV · 28 集')
    expect(button(container, '别的番').textContent).toContain('自动匹配')

    await act(async () => button(container, '葬送的芙莉莲').click())
    await settle()

    expect(api.setAssociation).toHaveBeenCalledWith('k1', { mode: 'manual', anilistId: 154587 })
    expect(onChanged).toHaveBeenCalled()
    expect(container.querySelector('dialog')?.hasAttribute('open')).toBe(false)
    await unmount()
  })

  it('改了关键词再搜；可以标为不是目录里的作品；保存失败留在对话框里说原因', async () => {
    api.fetchCatalogSearch.mockResolvedValue([])
    api.setAssociation.mockRejectedValueOnce(new Error('媒体库里已经没有这个作品分组了'))
    const { container, unmount } = await mount(<AssociationPanel cluster={cluster()} onChanged={vi.fn()} />)
    await act(async () => button(container, '选择作品').click())
    await settle()
    expect(container.textContent).toContain('没有找到')

    const input = container.querySelector<HTMLInputElement>('.assoc-search input')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, 'Frieren')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => button(container, '搜索').click())
    await settle()
    expect(api.fetchCatalogSearch).toHaveBeenLastCalledWith('Frieren', expect.any(AbortSignal))

    await act(async () => button(container, '不是目录里的作品').click())
    await settle()
    expect(api.setAssociation).toHaveBeenCalledWith('k1', { mode: 'none' })
    expect(container.querySelector('dialog [role="alert"]')?.textContent).toContain('已经没有这个作品分组')
    expect(container.querySelector('dialog')?.hasAttribute('open')).toBe(true)
    await unmount()
  })

  it('从对话框保存也走面板那条路：确认条与旧错误清掉，焦点回到打开它的按钮', async () => {
    api.fetchCatalogSearch.mockResolvedValue([frieren])
    api.setAssociation.mockRejectedValueOnce(new Error('旧错误')).mockResolvedValue({ mode: 'manual', anilistId: 154587, setAt: 2 })
    const c = cluster({ association: { mode: 'manual', anilistId: 1, title: '对的番', setAt: 1, syncedElsewhere: [{ anilistId: 999, episodes: [1] }] } })
    const onChanged = vi.fn()
    const { container, unmount } = await mount(<AssociationPanel cluster={c} onChanged={onChanged} />)
    // 先制造一个面板错误（确认条留着没关）
    await act(async () => button(container, '回到自动匹配').click())
    await act(async () => button(container, '确定删除并回到自动匹配').click())
    expect(container.querySelector('.assoc-panel > [role="alert"]')?.textContent).toContain('旧错误')
    expect(container.textContent).toContain('清单也会一起删掉')

    await act(async () => button(container, '更换').click())
    await settle()
    await act(async () => button(container, '葬送的芙莉莲').click())
    await settle()

    expect(onChanged).toHaveBeenCalled()
    expect(container.textContent).not.toContain('清单也会一起删掉')
    expect(container.querySelector('.assoc-panel > [role="alert"]')).toBeNull()
    expect(document.activeElement).toBe(button(container, '更换'))
    await unmount()
  })

  it('保存还没回来就关掉了对话框：失败原因改在面板上说，不悄悄丢掉', async () => {
    api.fetchCatalogSearch.mockResolvedValue([frieren])
    let fail = (_: Error) => {}
    api.setAssociation.mockReturnValue(new Promise((_, reject) => { fail = reject }))
    const { container, unmount } = await mount(<AssociationPanel cluster={cluster()} onChanged={vi.fn()} />)
    await act(async () => button(container, '选择作品').click())
    await settle()
    await act(async () => button(container, '葬送的芙莉莲').click())

    // 保存期间也能关
    await act(async () => container.querySelector<HTMLButtonElement>('.assoc-dialog-close')!.click())
    expect(container.querySelector('dialog')?.hasAttribute('open')).toBe(false)
    await act(async () => fail(new Error('animego 暂时不可达')))
    await settle()

    expect(container.querySelector('.assoc-panel > [role="alert"]')?.textContent).toContain('animego 暂时不可达')
    await unmount()
  })

  it('搜索还没回来就关掉对话框：重开时照样再搜一次（取消的请求不能把状态卡在「正在搜索」）', async () => {
    api.fetchCatalogSearch.mockReturnValueOnce(new Promise(() => {})).mockResolvedValue([frieren])
    const { container, unmount } = await mount(<AssociationPanel cluster={cluster()} onChanged={vi.fn()} />)
    await act(async () => button(container, '选择作品').click())
    await act(async () => container.querySelector<HTMLButtonElement>('.assoc-dialog-close')!.click())

    await act(async () => button(container, '选择作品').click())
    await settle()

    expect(api.fetchCatalogSearch).toHaveBeenCalledTimes(2)
    expect(container.querySelector('.assoc-result')?.textContent).toContain('葬送的芙莉莲')
    await unmount()
  })

  it('只有按下和松开都落在框体之外才算点遮罩（在搜索框里拖选、松手落到遮罩上不关）', async () => {
    api.fetchCatalogSearch.mockResolvedValue([])
    const { container, unmount } = await mount(<AssociationPanel cluster={cluster()} onChanged={vi.fn()} />)
    await act(async () => button(container, '选择作品').click())
    const dialog = container.querySelector('dialog')!
    dialog.getBoundingClientRect = () => ({ left: 100, right: 500, top: 100, bottom: 500, width: 400, height: 400, x: 100, y: 100, toJSON: () => ({}) })
    const at = (type: string, x: number, target: Element = dialog) =>
      act(async () => { target.dispatchEvent(new MouseEvent(type, { bubbles: true, clientX: x, clientY: x })) })

    await at('pointerdown', 200, container.querySelector('.assoc-search input')!)
    await at('click', 10)
    expect(dialog.hasAttribute('open')).toBe(true)

    await at('pointerdown', 300) // 框体内（比如边框）
    await at('click', 300)
    expect(dialog.hasAttribute('open')).toBe(true)

    await at('pointerdown', 10)
    await at('click', 10)
    expect(dialog.hasAttribute('open')).toBe(false)
    await unmount()
  })

  it('已经标为不是目录作品时，那个按钮不可再点；初始关键词截到 64 个字', async () => {
    api.fetchCatalogSearch.mockResolvedValue([])
    const long = '长'.repeat(80)
    const { container, unmount } = await mount(<AssociationPanel cluster={cluster({ title: long, association: { mode: 'none', setAt: 1 } })} onChanged={vi.fn()} />)
    await act(async () => button(container, '更换').click())
    await settle()

    expect(button(container, '不是目录里的作品').disabled).toBe(true)
    expect(api.fetchCatalogSearch).toHaveBeenCalledWith('长'.repeat(64), expect.any(AbortSignal))
    await unmount()
  })
})
