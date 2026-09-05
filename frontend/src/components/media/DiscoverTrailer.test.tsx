// @vitest-environment jsdom
import { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { DiscoverTrailer, TRAILER_HOVER_MS } from './DiscoverTrailer'

const ID = 'tR8YH0G67Rk'
const ORIGIN = 'https://www.youtube-nocookie.com'
beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())
async function advance(ms: number) { await act(async () => { vi.advanceTimersByTime(ms) }) }

describe('Discover 静音预告片', () => {
  it('悬停一秒后才加载；移开会卸载播放器并清除等待任务', async () => {
    const { container, rerender, unmount } = await mount(<DiscoverTrailer videoId={ID} active title="作品" />)
    expect(container.querySelector('iframe')).toBeNull()
    await advance(TRAILER_HOVER_MS)
    const iframe = container.querySelector('iframe')!
    const url = new URL(iframe.src)
    expect(url.origin).toBe(ORIGIN)
    expect(url.searchParams.get('mute')).toBe('1')
    expect(url.searchParams.get('autoplay')).toBe('1')
    expect(iframe.getAttribute('referrerpolicy')).toBe('origin')
    expect(container.querySelector('.hero-trailer--playing')).toBeNull()
    await rerender(<DiscoverTrailer videoId={ID} active={false} title="作品" />)
    expect(container.querySelector('iframe')).toBeNull()
    expect(vi.getTimerCount()).toBe(0)
    await unmount()
  })

  it('只相信当前 YouTube iframe 的播放消息；失败恢复图片', async () => {
    const { container, unmount } = await mount(<DiscoverTrailer videoId={ID} active title="作品" />)
    await advance(TRAILER_HOVER_MS)
    const frame = container.querySelector('iframe')!
    const playing = JSON.stringify({ event: 'infoDelivery', info: { playerState: 1 } })
    await act(async () => window.dispatchEvent(new MessageEvent('message', { origin: 'https://example.com', source: frame.contentWindow, data: playing })))
    expect(container.querySelector('.hero-trailer--playing')).toBeNull()
    await act(async () => window.dispatchEvent(new MessageEvent('message', { origin: ORIGIN, source: window, data: playing })))
    expect(container.querySelector('.hero-trailer--playing')).toBeNull()
    await act(async () => window.dispatchEvent(new MessageEvent('message', { origin: ORIGIN, source: frame.contentWindow, data: playing })))
    expect(container.querySelector('.hero-trailer--playing')).toBeNull()
    await act(async () => frame.dispatchEvent(new Event('load')))
    await advance(999)
    expect(container.querySelector('.hero-trailer--playing')).toBeNull()
    await advance(1)
    expect(container.querySelector('.hero-trailer--playing')).not.toBeNull()
    expect(vi.getTimerCount()).toBe(0)
    await act(async () => window.dispatchEvent(new MessageEvent('message', { origin: ORIGIN, source: frame.contentWindow, data: JSON.stringify({ event: 'onError', info: 150 }) })))
    expect(container.querySelector('iframe')).toBeNull()
    expect(container.textContent).toContain('预告片暂不可用')
    await unmount()
  })

  it('无效 ID 不加载，超时不会显示空白视频', async () => {
    const { container, rerender, unmount } = await mount(<DiscoverTrailer videoId="https://example.com/video" active title="作品" />)
    await advance(20000)
    expect(container.querySelector('iframe')).toBeNull()
    await rerender(<DiscoverTrailer videoId={ID} active title="作品" />)
    await advance(TRAILER_HOVER_MS)
    await advance(15000)
    expect(container.querySelector('iframe')).toBeNull()
    expect(container.textContent).toContain('预告片暂不可用')
    expect(vi.getTimerCount()).toBe(0)
    await unmount()
  })

  it('卡片预告片立即加载，快速移出不会留下延迟播放', async () => {
    const { container, rerender, unmount } = await mount(<DiscoverTrailer videoId={ID} active title="作品" variant="card" />)
    await advance(0)
    const frame = container.querySelector('iframe')!
    expect(frame).not.toBeNull()
    await act(async () => frame.dispatchEvent(new Event('load')))
    await rerender(<DiscoverTrailer videoId={ID} active={false} title="作品" variant="card" />)
    await advance(2000)
    expect(container.querySelector('iframe')).toBeNull()
    expect(vi.getTimerCount()).toBe(0)
    await unmount()
  })
})
