// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { copyText } from './clipboard'

function stubClipboard(writeText: (text: string) => Promise<void>): void {
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'clipboard')
  Reflect.deleteProperty(document, 'execCommand')
  vi.restoreAllMocks()
})

describe('copyText', () => {
  it('navigator.clipboard 可用时直接写入', async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>().mockResolvedValue(undefined)
    stubClipboard(writeText)
    await expect(copyText('brew install mpv')).resolves.toBe(true)
    expect(writeText).toHaveBeenCalledExactlyOnceWith('brew install mpv')
  })

  it('clipboard 被拒时回退到 execCommand，成功则算成功', async () => {
    stubClipboard(vi.fn().mockRejectedValue(new Error('NotAllowedError')))
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const exec = vi.fn(() => true)
    Object.defineProperty(document, 'execCommand', { value: exec, configurable: true })

    await expect(copyText('x')).resolves.toBe(true)
    expect(exec).toHaveBeenCalledExactlyOnceWith('copy')
    expect(errorSpy).toHaveBeenCalled()
    // 临时 textarea 用完即删
    expect(document.querySelector('textarea')).toBeNull()
  })

  it('没有 clipboard 也没有 execCommand 时返回 false 并留下原因', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    await expect(copyText('x')).resolves.toBe(false)
    expect(errorSpy).toHaveBeenCalled()
  })

  it('execCommand 返回 false 时算失败', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    Object.defineProperty(document, 'execCommand', { value: () => false, configurable: true })
    await expect(copyText('x')).resolves.toBe(false)
  })
})
