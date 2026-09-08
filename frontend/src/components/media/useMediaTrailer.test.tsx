// @vitest-environment jsdom
import { act } from 'react'
import { beforeEach, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { fetchMedia } from '../../lib/media'
import { useMediaTrailer } from './useMediaTrailer'
import type { MediaSummary } from './types'
vi.mock('../../lib/media', () => ({ fetchMedia: vi.fn() }))
const media: MediaSummary = { id: 1, title: '作品', episodes: null, genres: [], watched: 0 }
function Probe({ item = media, active = false }: { item?: MediaSummary; active?: boolean }) {
  const id = useMediaTrailer(item, active)
  return <span>{id ?? 'poster'}</span>
}
beforeEach(() => vi.mocked(fetchMedia).mockReset())
it('只在悬停且缺少预告片时通过作品详情补齐，已有 ID 不额外请求', async () => {
 vi.mocked(fetchMedia).mockResolvedValue({ ...media, trailerId: 'abcdefghijk' })
 const ui = await mount(<Probe />)
 expect(fetchMedia).not.toHaveBeenCalled()
 await ui.rerender(<Probe active />)
 expect(fetchMedia).toHaveBeenCalledTimes(1); expect(ui.container.textContent).toBe('abcdefghijk')
 await ui.rerender(<Probe item={{ ...media, id: 2, trailerId: 'newabcdefgh' }} active />)
 expect(fetchMedia).toHaveBeenCalledTimes(1); expect(ui.container.textContent).toBe('newabcdefgh')
 await ui.unmount()
})
it('切换卡片后旧详情不能把上一部预告片带进新卡片', async () => {
 let finish!: (m: MediaSummary) => void
 vi.mocked(fetchMedia).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
 const ui = await mount(<Probe active />)
 await ui.rerender(<Probe item={{ ...media, id: 2 }} />)
 await act(async () => finish({ ...media, trailerId: 'abcdefghijk' }))
 expect(ui.container.textContent).toBe('poster')
 await ui.unmount()
})
