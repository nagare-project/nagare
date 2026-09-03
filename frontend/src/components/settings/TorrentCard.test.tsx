// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { TorrentConfigData, TorrentSettings } from '../../lib/endpoints'
import { mount } from '../../test/harness'

const endpoints = vi.hoisted(() => ({
  updateTorrentConfig: vi.fn(),
  clearTorrentCache: vi.fn(),
}))
vi.mock('../../lib/endpoints', () => endpoints)

import { ENGINE_DOWN_HINT, PORT_HINT, TorrentCard } from './TorrentCard'

const SETTINGS: TorrentSettings = {
  enabled: true,
  seeding: false,
  trackers: [],
  portForwarding: true,
  listenPort: 6881,
  cacheDir: '/Users/you/Library/Application Support/nagare/cache/torrent',
  cacheBytes: 5_505_024, // 5.25 MB
}

function configResult(overrides: Partial<TorrentConfigData> = {}): TorrentConfigData {
  return { ...SETTINGS, restartRequired: false, ...overrides }
}

const mounted: Array<() => Promise<void>> = []

async function mountCard(torrent: TorrentSettings = SETTINGS) {
  const onReload = vi.fn<() => Promise<void>>().mockResolvedValue(undefined)
  const result = await mount(<TorrentCard torrent={torrent} onReload={onReload} />)
  mounted.push(result.unmount)
  return { ...result, onReload }
}

function button(container: HTMLElement, text: string): HTMLButtonElement {
  const found = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === text)
  if (found === undefined) throw new Error(`找不到按钮「${text}」`)
  return found
}

function toggle(container: HTMLElement, name: string): HTMLButtonElement {
  const found = container.querySelector<HTMLButtonElement>(`button[aria-label="${name}"]`)
  if (found === null) throw new Error(`找不到开关「${name}」`)
  return found
}

function field<T extends HTMLInputElement | HTMLTextAreaElement>(
  container: HTMLElement,
  id: string,
): T {
  const found = container.querySelector<T>(`#${id}`)
  if (found === null) throw new Error(`找不到输入框「${id}」`)
  return found
}

/** 受控输入：走原生 setter + input 事件，React 才收得到 */
async function type(element: HTMLInputElement | HTMLTextAreaElement, value: string): Promise<void> {
  const proto =
    element instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : HTMLInputElement.prototype
  const setter = Object.getOwnPropertyDescriptor(proto, 'value')?.set
  await act(async () => {
    setter?.call(element, value)
    element.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

async function submit(container: HTMLElement): Promise<void> {
  await act(async () => {
    button(container, '保存').click()
  })
}

const status = (container: HTMLElement): string =>
  container.querySelector('.result[role="status"]')?.textContent ?? ''

beforeEach(() => {
  endpoints.updateTorrentConfig.mockResolvedValue(configResult())
  endpoints.clearTorrentCache.mockResolvedValue({ cacheBytes: 0 })
})

afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.resetAllMocks()
  vi.restoreAllMocks()
})

describe('TorrentCard · 受控行为', () => {
  it('开关与输入按后端当前值初始化，没改动时不提示「未保存」', async () => {
    const { container } = await mountCard()
    expect(toggle(container, '持续做种').getAttribute('aria-checked')).toBe('false')
    expect(toggle(container, '自动端口映射').getAttribute('aria-checked')).toBe('true')
    expect(field<HTMLInputElement>(container, 'torrent-listen-port').value).toBe('6881')
    expect(field<HTMLTextAreaElement>(container, 'torrent-trackers').value).toBe('')
    expect(container.querySelector('.torrent-dirty')).toBeNull()
  })

  it('拨动开关立即反映在 aria-checked 上，并提示「有未保存的改动」', async () => {
    const { container } = await mountCard()
    await act(async () => {
      toggle(container, '持续做种').click()
    })
    expect(toggle(container, '持续做种').getAttribute('aria-checked')).toBe('true')
    expect(container.querySelector('.torrent-dirty')?.textContent).toBe('有未保存的改动')
  })

  it('说明写清楚「持续做种」只管停止播放之后的上传', async () => {
    const { container } = await mountCard()
    const note = container.querySelectorAll('.torrent-toggle-note')[0]?.textContent ?? ''
    expect(note).toContain('BT 协议必需')
    expect(note).toContain('停止播放之后是否继续上传')
  })

  it('tracker 说明写明留空即只用 DHT/PEX，且只补给公开种子', async () => {
    const { container } = await mountCard()
    const note = container.querySelector('#torrent-trackers-note')?.textContent ?? ''
    expect(note).toContain('DHT / PEX')
    expect(note).toContain('公开种子')
  })

  it('保存把四项一起发出去；tracker 按行拆分并丢掉空行', async () => {
    const { container, onReload } = await mountCard()
    await act(async () => {
      toggle(container, '持续做种').click()
    })
    await type(field(container, 'torrent-trackers'), 'udp://a.invalid:80\n\n  udp://b.invalid:80  \n')
    await submit(container)

    expect(endpoints.updateTorrentConfig).toHaveBeenCalledExactlyOnceWith({
      seeding: true,
      portForwarding: true,
      listenPort: 6881,
      trackers: ['udp://a.invalid:80', 'udp://b.invalid:80'],
    })
    expect(onReload).toHaveBeenCalledTimes(1)
    expect(status(container)).toBe('已保存')
  })

  it('保存后以后端返回值回填，「未保存」提示消失', async () => {
    endpoints.updateTorrentConfig.mockResolvedValue(
      configResult({ seeding: true, listenPort: 51413, trackers: ['udp://a.invalid:80'] }),
    )
    const { container } = await mountCard()
    await act(async () => {
      toggle(container, '持续做种').click()
    })
    await submit(container)

    expect(field<HTMLInputElement>(container, 'torrent-listen-port').value).toBe('51413')
    expect(field<HTMLTextAreaElement>(container, 'torrent-trackers').value).toBe('udp://a.invalid:80')
    expect(container.querySelector('.torrent-dirty')).toBeNull()
  })

  it('保存失败：显示后端中文错误，不刷新设置', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    endpoints.updateTorrentConfig.mockRejectedValue(new Error('监听端口已被占用'))
    const { container, onReload } = await mountCard()
    await submit(container)
    expect(container.querySelector('.result--err[role="status"]')?.textContent).toBe('监听端口已被占用')
    expect(onReload).not.toHaveBeenCalled()
  })
})

describe('TorrentCard · 端口校验', () => {
  it.each<[string]>([['65536'], ['-1'], ['abc'], [''], ['80.5']])(
    '非法端口 %j 不发请求，给出中文说明',
    async (value) => {
      const { container } = await mountCard()
      await type(field(container, 'torrent-listen-port'), value)
      await submit(container)
      expect(endpoints.updateTorrentConfig).not.toHaveBeenCalled()
      expect(container.querySelector('.result--err[role="status"]')?.textContent).toBe(PORT_HINT)
    },
  )

  it.each<[string, number]>([
    ['0', 0],
    ['1', 1],
    ['65535', 65535],
    [' 51413 ', 51413],
  ])('合法端口 %j → %s', async (value, expected) => {
    const { container } = await mountCard()
    await type(field(container, 'torrent-listen-port'), value)
    await submit(container)
    expect(endpoints.updateTorrentConfig).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ listenPort: expected }),
    )
  })
})

describe('TorrentCard · 重启提示', () => {
  it('restartRequired=true 时提示重启后生效', async () => {
    endpoints.updateTorrentConfig.mockResolvedValue(
      configResult({ listenPort: 51413, restartRequired: true }),
    )
    const { container } = await mountCard()
    await type(field(container, 'torrent-listen-port'), '51413')
    await submit(container)
    const notice = container.querySelector('.torrent-restart[role="alert"]')
    expect(notice?.textContent).toContain('重启 nagare 后生效')
  })

  it('restartRequired=false 时不出重启提示', async () => {
    const { container } = await mountCard()
    await submit(container)
    expect(container.querySelector('.torrent-restart')).toBeNull()
  })
})

describe('TorrentCard · 缓存', () => {
  it('显示缓存占用与目录，并说明当前清理策略', async () => {
    const { container } = await mountCard()
    const text = container.textContent ?? ''
    expect(text).toContain('5.25 MB')
    expect(text).toContain(SETTINGS.cacheDir)
    expect(text).toContain('停止播放即删除该种子的分片，启动与退出时各清空一次')
  })

  it('「清空缓存」调接口、按返回值更新占用并刷新设置', async () => {
    const { container, onReload } = await mountCard()
    await act(async () => {
      button(container, '清空缓存').click()
    })
    expect(endpoints.clearTorrentCache).toHaveBeenCalledTimes(1)
    expect(onReload).toHaveBeenCalledTimes(1)
    expect(status(container)).toBe('已清空缓存 · 当前占用 0 B')
    expect(container.querySelector('.kv-list dd')?.textContent).toBe('0 B')
  })

  it('清空失败：显示中文错误', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    endpoints.clearTorrentCache.mockRejectedValue(new Error('缓存目录里有文件被占用，删不掉'))
    const { container } = await mountCard()
    await act(async () => {
      button(container, '清空缓存').click()
    })
    expect(container.querySelector('.result--err[role="status"]')?.textContent).toBe(
      '缓存目录里有文件被占用，删不掉',
    )
  })
})

describe('TorrentCard · 引擎不可用', () => {
  it('顶部给出明确提示，所有开关与按钮禁用（不能只是点了没反应）', async () => {
    const { container } = await mountCard({ ...SETTINGS, enabled: false })
    expect(container.querySelector('.alert-warn[role="alert"]')?.textContent).toBe(ENGINE_DOWN_HINT)

    expect(toggle(container, '持续做种').disabled).toBe(true)
    expect(toggle(container, '自动端口映射').disabled).toBe(true)
    expect(field<HTMLInputElement>(container, 'torrent-listen-port').disabled).toBe(true)
    expect(field<HTMLTextAreaElement>(container, 'torrent-trackers').disabled).toBe(true)
    expect(button(container, '保存').disabled).toBe(true)
    expect(button(container, '清空缓存').disabled).toBe(true)
  })

  it('引擎正常时不出这条提示', async () => {
    const { container } = await mountCard()
    expect(container.querySelector('.alert-warn')).toBeNull()
  })
})
