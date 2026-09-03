// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TorrentPlayState } from '../../hooks/useTorrentPlay'
import type { TorrentPhase, TorrentStatus } from '../../lib/endpoints'
import { mount } from '../../test/harness'
import { TorrentStatusBar, ZERO_PEER_ALERT_SECONDS } from './TorrentStatusBar'

const MAGNET = 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567'
const TITLE = '[Sakurato] Sousou no Frieren [01][1080p]'

const STARTING: TorrentPlayState = { phase: 'starting', magnet: MAGNET, title: TITLE }

function makeStatus(overrides: Partial<TorrentStatus> = {}): TorrentStatus {
  return {
    active: true,
    phase: 'metadata',
    peers: 4,
    seeders: 2,
    downRate: 1_572_864, // 1.5 MB/s
    upRate: 0,
    buffered: 0,
    progress: 0,
    cacheBytes: 0,
    seeding: false,
    ...overrides,
  }
}

const mounted: Array<() => Promise<void>> = []

async function mountBar(overrides: Partial<Parameters<typeof TorrentStatusBar>[0]> = {}) {
  const props = {
    state: STARTING,
    status: makeStatus(),
    zeroPeerSeconds: 0,
    onCancel: vi.fn<() => void>(),
    onRetry: vi.fn<() => void>(),
    ...overrides,
  }
  const result = await mount(<TorrentStatusBar {...props} />)
  mounted.push(result.unmount)
  return { ...result, props }
}

function button(container: HTMLElement, text: string): HTMLButtonElement {
  const found = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === text)
  if (found === undefined) throw new Error(`找不到按钮「${text}」`)
  return found
}

afterEach(async () => {
  while (mounted.length > 0) await mounted.pop()?.()
  vi.restoreAllMocks()
})

describe('TorrentStatusBar · 阶段文案', () => {
  it('idle 时整条不渲染', async () => {
    const { container } = await mountBar({ state: { phase: 'idle' }, status: null })
    expect(container.querySelector('.tsb-bar')).toBeNull()
  })

  it.each<[TorrentPhase, string]>([
    ['metadata', '正在查找分享者 …'],
    ['selecting', '正在读取种子信息 …'],
    ['ready', '即将开始播放'],
  ])('phase=%s → 「%s」', async (phase, expected) => {
    const { container } = await mountBar({ status: makeStatus({ phase }) })
    expect(container.querySelector('.tsb-phase')?.textContent).toBe(expected)
    expect(container.querySelector('[role="status"]')?.textContent).toBe(expected)
  })

  it('phase=buffering：可见文案带百分比，进度条同步；读屏只播报粗粒度阶段', async () => {
    const { container } = await mountBar({
      status: makeStatus({ phase: 'buffering', buffered: 0.62 }),
    })
    expect(container.querySelector('.tsb-phase')?.textContent).toBe('正在缓冲 62%')
    expect(container.querySelector<HTMLElement>('.tsb-track-fill')?.style.width).toBe('62%')
    expect(container.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow')).toBe('62')
    // 百分比每秒变一次，不能进 live region，否则读屏一秒念一遍
    expect(container.querySelector('[role="status"]')?.textContent).toBe('正在缓冲')
  })

  it('还没拉到状态时显示「正在启动 …」而不是空白', async () => {
    const { container } = await mountBar({ status: null })
    expect(container.querySelector('.tsb-phase')?.textContent).toBe('正在启动 …')
    expect(container.querySelector('.tsb-readout')?.textContent).toBe('状态未知')
  })

  it('等待选集时文案切换成「请选择要播放的剧集」', async () => {
    const { container } = await mountBar({
      state: { phase: 'selecting', magnet: MAGNET, title: TITLE, files: [] },
      status: makeStatus({ phase: 'selecting' }),
    })
    expect(container.querySelector('.tsb-phase')?.textContent).toBe('请选择要播放的剧集')
  })

  it('标题优先显示后端已选中的文件名', async () => {
    const { container } = await mountBar({
      status: makeStatus({ fileName: 'Frieren - 01.mkv' }),
    })
    expect(container.querySelector('.tsb-title')?.textContent).toBe('Frieren - 01.mkv')
  })
})

describe('TorrentStatusBar · 分享者与速度', () => {
  it('显示分享者数、做种数与下载速度', async () => {
    const { container } = await mountBar()
    const readout = container.querySelector('.tsb-readout')
    expect(readout?.textContent).toContain('分享者 4')
    expect(readout?.textContent).toContain('做种 2')
    expect(readout?.textContent).toContain('1.5 MB/s')
    expect(readout?.classList.contains('tsb-readout--nopeers')).toBe(false)
  })

  it('peers=0 立刻转警示色（不藏进 tooltip）', async () => {
    const { container } = await mountBar({ status: makeStatus({ peers: 0, seeders: 0 }) })
    const readout = container.querySelector('.tsb-readout')
    expect(readout?.textContent).toContain('分享者 0')
    expect(readout?.classList.contains('tsb-readout--nopeers')).toBe(true)
    // 还没到阈值：只变色，不弹警告
    expect(container.querySelector('.tsb-alert')).toBeNull()
  })

  it('peers 长期为 0：给出显式警告与「换一条资源」的动作建议', async () => {
    const { container } = await mountBar({
      status: makeStatus({ peers: 0, seeders: 0 }),
      zeroPeerSeconds: ZERO_PEER_ALERT_SECONDS,
    })
    const alert = container.querySelector('.tsb-alert[role="alert"]')
    expect(alert?.textContent).toContain('一直没有连接到任何分享者')
    expect(alert?.textContent).toContain(`已 ${ZERO_PEER_ALERT_SECONDS} 秒`)
    expect(alert?.textContent).toContain('换一条')
    // 每秒都在变的秒数对读屏隐藏，避免 role=alert 被反复播报
    expect(alert?.querySelector('[aria-hidden="true"]')?.textContent).toBe(
      `（已 ${ZERO_PEER_ALERT_SECONDS} 秒）`,
    )
  })
})

describe('TorrentStatusBar · 动作', () => {
  it('缓冲中点「取消」调 onCancel', async () => {
    const { container, props } = await mountBar()
    await act(async () => {
      button(container, '取消').click()
    })
    expect(props.onCancel).toHaveBeenCalledTimes(1)
    expect(props.onRetry).not.toHaveBeenCalled()
  })

  it('失败态：原样显示后端中文错误 + 重试 + 关闭', async () => {
    const message = '找不到可用的分享者，换一条资源试试'
    const { container, props } = await mountBar({
      state: { phase: 'error', magnet: MAGNET, title: TITLE, message },
      status: null,
    })
    expect(container.querySelector('.tsb-phase[role="alert"]')?.textContent).toBe(message)

    await act(async () => {
      button(container, '重试').click()
    })
    expect(props.onRetry).toHaveBeenCalledTimes(1)

    await act(async () => {
      button(container, '关闭').click()
    })
    expect(props.onCancel).toHaveBeenCalledTimes(1)
  })
})

describe('TorrentStatusBar · 播放开始后收敛', () => {
  it('streaming：收成轻量指示，仍显示速度 / 分享者 / 已下载，按钮变「停止」', async () => {
    const { container, props } = await mountBar({
      state: { phase: 'streaming', magnet: MAGNET, title: TITLE, danmaku: { state: 'ok', count: 3210 } },
      status: makeStatus({ phase: 'ready', progress: 0.34, peers: 5, seeders: 3 }),
    })

    expect(container.querySelector('.tsb-bar')?.classList.contains('tsb-bar--streaming')).toBe(true)
    expect(container.querySelector('.tsb-phase')?.textContent).toBe('正在边下边播')
    // 起播缓冲进度条撤掉（已经在播了），但下载读数保留
    expect(container.querySelector('.tsb-track')).toBeNull()
    const readout = container.querySelector('.tsb-readout')
    expect(readout?.textContent).toContain('分享者 5')
    expect(readout?.textContent).toContain('1.5 MB/s')
    expect(readout?.textContent).toContain('已下载 34%')

    await act(async () => {
      button(container, '停止').click()
    })
    expect(props.onCancel).toHaveBeenCalledTimes(1)
  })
})

describe('TorrentStatusBar · 会话级失败', () => {
  // 磁盘写不进去时，「没有分享者」那条提示会把用户引向完全错误的下一步
  //（去换资源），所以会话错误必须压过它。
  it('写盘失败的提示压过分享者告警', async () => {
    const { container, unmount } = await mountBar({
      state: { phase: 'streaming', magnet: MAGNET, title: TITLE, danmaku: { state: 'ok' } },
      status: makeStatus({
        active: true,
        phase: 'ready',
        peers: 0,
        error: '磁盘写入失败，已停止下载。确认磁盘还有空间、缓存目录可写，然后重新播放',
      }),
      zeroPeerSeconds: 60,
    })

    const alerts = Array.from(container.querySelectorAll('.tsb-alert'))
    expect(alerts).toHaveLength(1)
    expect(alerts[0]?.textContent).toContain('磁盘写入失败')
    expect(alerts[0]?.textContent).not.toContain('分享者')

    await unmount()
  })

  it('没有会话错误时仍然显示分享者告警', async () => {
    const { container, unmount } = await mountBar({
      state: { phase: 'streaming', magnet: MAGNET, title: TITLE, danmaku: { state: 'ok' } },
      status: makeStatus({ active: true, phase: 'ready', peers: 0 }),
      zeroPeerSeconds: 60,
    })

    const alert = container.querySelector('.tsb-alert')
    expect(alert?.textContent).toContain('分享者')

    await unmount()
  })
})
