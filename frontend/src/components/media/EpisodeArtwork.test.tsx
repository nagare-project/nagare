// @vitest-environment jsdom
import { act } from 'react'
import { describe, expect, it } from 'vitest'
import { mount } from '../../test/harness'
import { EpisodeArtwork } from './EpisodeArtwork'

describe('逐集截图', () => {
  it('优先真实截图，失败后依次回退横图和海报', async () => {
    const { container, unmount } = await mount(<EpisodeArtwork image="/still.jpg" banner="/banner.jpg" cover="/cover.jpg" />)
    const src = () => container.querySelector('img')?.getAttribute('src')
    expect(src()).toBe('/still.jpg')
    await act(async () => container.querySelector('img')!.dispatchEvent(new Event('error')))
    expect(src()).toBe('/banner.jpg')
    await act(async () => container.querySelector('img')!.dispatchEvent(new Event('error')))
    expect(src()).toBe('/cover.jpg')
    await act(async () => container.querySelector('img')!.dispatchEvent(new Event('error')))
    expect(container.textContent).toBe('暂无图片')
    await unmount()
  })
  it('切换图片后重新尝试新地址', async () => {
    const { container, rerender, unmount } = await mount(<EpisodeArtwork image="/old.jpg" />)
    await act(async () => container.querySelector('img')!.dispatchEvent(new Event('error')))
    await rerender(<EpisodeArtwork image="/next.jpg" />)
    expect(container.querySelector('img')?.getAttribute('src')).toBe('/next.jpg')
    await unmount()
  })
})
