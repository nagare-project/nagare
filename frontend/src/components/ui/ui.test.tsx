// @vitest-environment jsdom
import { act, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { mount } from '../../test/harness'
import { Badge, Button, Input, Panel, Result, Switch, Tab, TabCount, Tabs } from './index'

/**
 * 原子件层的测试。
 *
 * 这里【不】测样式 —— 类名与手写时期逐字一致，页面测试已经覆盖了。
 * 这里测的是封装本身买到的那些东西：每个原子件都对应一条
 * 「手写时几乎总会漏、漏了又只对少数人可见」的细节。
 * 它们没有测试就会在下一次重构里悄悄消失。
 */

describe('Button', () => {
  it('type 默认是 button，不会在表单里变成意外提交', async () => {
    // 表单里漏写 type 的 <button> 会提交表单 —— 最容易忽略、也最容易出事的默认值
    const onSubmit = vi.fn((e: Event) => e.preventDefault())
    const { container, unmount } = await mount(
      <form>
        <Button>点我</Button>
      </form>,
    )
    container.querySelector('form')?.addEventListener('submit', onSubmit)
    await act(async () => container.querySelector('button')?.click())
    expect(onSubmit).not.toHaveBeenCalled()
    expect(container.querySelector('button')?.getAttribute('type')).toBe('button')
    await unmount()
  })

  it('意图与尺寸翻成与手写时期一致的类名', async () => {
    const { container, unmount } = await mount(
      <>
        <Button>次要</Button>
        <Button intent="primary" size="sm">
          主
        </Button>
        <Button intent="danger">危险</Button>
      </>,
    )
    const cls = [...container.querySelectorAll('button')].map((b) => b.className)
    expect(cls[0]).toBe('btn')
    expect(cls[1]).toBe('btn btn--sm btn--primary')
    expect(cls[2]).toBe('btn btn--danger')
    await unmount()
  })
})

describe('Result', () => {
  it('错误与警示默认是 live region —— 异步冒出来的报错读屏必须被告知', async () => {
    const { container, unmount } = await mount(
      <>
        <Result tone="err">炸了</Result>
        <Result tone="warn">当心</Result>
        <Result tone="dim">普通</Result>
      </>,
    )
    const [err, warn, dim] = [...container.querySelectorAll('p')]
    expect(err?.getAttribute('role')).toBe('status')
    expect(err?.getAttribute('aria-live')).toBe('polite')
    expect(warn?.getAttribute('role')).toBe('status')
    // 普通提示不该占用 live region，否则每次无关变化都会打断读屏
    expect(dim?.getAttribute('role')).toBeNull()
    await unmount()
  })
})

describe('Input', () => {
  it('标签与输入框绑死 —— 缺标签的输入框读屏只会念「编辑框」', async () => {
    const { container, unmount } = await mount(
      <Input id="email" label="邮箱" defaultValue="a@b.c" />,
    )
    const label = container.querySelector('label')
    const input = container.querySelector('input')
    expect(label?.getAttribute('for')).toBe('email')
    expect(input?.id).toBe('email')
    expect(label?.textContent).toBe('邮箱')
    await unmount()
  })

  it('visuallyHidden 只是视觉隐藏，标签仍在 DOM 里', async () => {
    const { container, unmount } = await mount(
      <Input id="q" label="搜索关键词" visuallyHidden />,
    )
    const label = container.querySelector('label')
    expect(label?.className).toBe('visually-hidden')
    expect(label?.textContent).toBe('搜索关键词')
    await unmount()
  })
})

describe('Switch', () => {
  it('是 role=switch 而不是自绘 checkbox，且必须有名字', async () => {
    function Host() {
      const [on, setOn] = useState(false)
      return <Switch checked={on} onChange={setOn} label="持续做种" />
    }
    const { container, unmount } = await mount(<Host />)
    const sw = container.querySelector('button')
    expect(sw?.getAttribute('role')).toBe('switch')
    expect(sw?.getAttribute('aria-checked')).toBe('false')
    // 只有一个小圆点的开关，没有 aria-label 读屏什么都读不出来
    expect(sw?.getAttribute('aria-label')).toBe('持续做种')

    await act(async () => sw?.click())
    expect(container.querySelector('button')?.getAttribute('aria-checked')).toBe('true')
    await unmount()
  })
})

describe('Panel', () => {
  it('面板与它的标题自动关联，读屏进入时先听到这是什么', async () => {
    const { container, unmount } = await mount(<Panel heading="磁力源">内容</Panel>)
    const section = container.querySelector('section')
    const heading = container.querySelector('h2')
    expect(section?.getAttribute('aria-labelledby')).toBe(heading?.id)
    expect(heading?.id).toBeTruthy()
    await unmount()
  })

  it('没有标题时不硬塞 aria-labelledby（指向不存在的 id 比没有更糟）', async () => {
    const { container, unmount } = await mount(<Panel>只是个容器</Panel>)
    expect(container.querySelector('section')?.getAttribute('aria-labelledby')).toBeNull()
    await unmount()
  })
})

describe('Tabs', () => {
  function Host() {
    const [v, setV] = useState('a')
    return (
      <Tabs value={v} onChange={setV} label="档位">
        <Tab value="a">甲</Tab>
        <Tab value="b">
          乙<TabCount n={3} />
        </Tab>
        <Tab value="c">丙</Tab>
      </Tabs>
    )
  }

  it('只有当前档进 Tab 键序列，其余用箭头走（WAI-ARIA 的 tablist 约定）', async () => {
    const { container, unmount } = await mount(<Host />)
    const tabs = [...container.querySelectorAll<HTMLButtonElement>('[role="tab"]')]
    expect(tabs.map((t) => t.tabIndex)).toEqual([0, -1, -1])
    expect(tabs.map((t) => t.getAttribute('aria-selected'))).toEqual(['true', 'false', 'false'])
    await unmount()
  })

  it('右箭头切到下一档并回绕 —— 手写版本两处都没有这个', async () => {
    const { container, unmount } = await mount(<Host />)
    const list = container.querySelector('[role="tablist"]')!
    const tabs = () => [...container.querySelectorAll<HTMLButtonElement>('[role="tab"]')]

    tabs()[0]!.focus()
    await act(async () => {
      list.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    })
    expect(tabs()[1]!.getAttribute('aria-selected')).toBe('true')

    // 从最后一档再按右箭头要回到第一档
    tabs()[2]!.focus()
    await act(async () => {
      list.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    })
    expect(tabs()[0]!.getAttribute('aria-selected')).toBe('true')
    await unmount()
  })

  it('Home / End 跳到两端', async () => {
    const { container, unmount } = await mount(<Host />)
    const list = container.querySelector('[role="tablist"]')!
    const tabs = () => [...container.querySelectorAll<HTMLButtonElement>('[role="tab"]')]

    await act(async () => {
      list.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true }))
    })
    expect(tabs()[2]!.getAttribute('aria-selected')).toBe('true')

    await act(async () => {
      list.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }))
    })
    expect(tabs()[0]!.getAttribute('aria-selected')).toBe('true')
    await unmount()
  })

  it('放在 Tabs 外面的 Tab 直接报错，而不是静默渲染成一个死按钮', async () => {
    // 静默失败在这里代价很高：一个点了没反应的标签，排查时根本不知道从哪找
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    await expect(mount(<Tab value="x">孤儿</Tab>)).rejects.toThrow('必须放在 Tabs 里面')
    spy.mockRestore()
  })
})

describe('Badge', () => {
  it('语义色走 tone，不在调用处拼类名', async () => {
    const { container, unmount } = await mount(
      <>
        <Badge>中性</Badge>
        <Badge tone="accent">强调</Badge>
        <Badge tone="warn" title="说明">
          警示
        </Badge>
      </>,
    )
    const cls = [...container.querySelectorAll('span')].map((s) => s.className)
    expect(cls).toEqual(['badge', 'badge badge--accent', 'badge badge--warn'])
    expect(container.querySelectorAll('span')[2]?.getAttribute('title')).toBe('说明')
    await unmount()
  })
})
