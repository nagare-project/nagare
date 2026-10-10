// @vitest-environment jsdom
import { act } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { updatePlayerConfig } from '../../lib/endpoints'
import { PlaybackCard } from './PlaybackCard'

vi.mock('../../lib/endpoints', async original => ({
  ...await original<typeof import('../../lib/endpoints')>(),
  updatePlayerConfig: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(updatePlayerConfig).mockReset()
})

const preset = (container: HTMLElement, label: string) =>
  [...container.querySelectorAll<HTMLButtonElement>('.playback-preset')].find(item => item.querySelector('strong')?.textContent === label)!
const status = (container: HTMLElement) => container.querySelector('[role="status"]')?.textContent

describe('设置页「画质增强」', () => {
  it('点一下就保存；正在播放时立刻套上，没在播就说下次生效', async () => {
    vi.mocked(updatePlayerConfig).mockResolvedValueOnce({ anime4k: 'hq', applied: true })
      .mockResolvedValueOnce({ anime4k: 'off', applied: false })
    const { container, unmount } = await mount(<PlaybackCard player={{ anime4k: 'off' }} />)
    expect(preset(container, '关').getAttribute('aria-pressed')).toBe('true')

    await act(async () => preset(container, '高质量').click())
    expect(updatePlayerConfig).toHaveBeenCalledExactlyOnceWith({ anime4k: 'hq' })
    expect(preset(container, '高质量').getAttribute('aria-pressed')).toBe('true')
    expect(status(container)).toBe('已套到正在播放的窗口上')

    await act(async () => preset(container, '关').click())
    expect(status(container)).toBe('已保存，下次开始播放时生效')
    await unmount()
  })

  it('设置存下了但窗口没换成功：照实说；保存失败：退回原来的选项', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(updatePlayerConfig).mockResolvedValueOnce({ anime4k: 'fast', applied: false, applyError: '没能把画质增强套到正在播放的窗口上。下次开始播放时会按新设置加载' })
      .mockRejectedValueOnce(new Error('后端断开'))
    const { container, unmount } = await mount(<PlaybackCard player={{ anime4k: 'off' }} />)
    await act(async () => preset(container, '标准').click())
    expect(status(container)).toContain('下次开始播放时会按新设置加载')

    await act(async () => preset(container, '高质量').click())
    expect(status(container)).toContain('后端断开')
    expect(preset(container, '标准').getAttribute('aria-pressed')).toBe('true')
    await unmount()
  })
})
