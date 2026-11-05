// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from './harness'
import { accessibleName, getAllByRole, getByRole, getByText, queryByRole } from './queries'

describe('按角色与文字找元素（测试工具）', () => {
  it('名字：aria-labelledby > aria-label > 内容文字（去掉 aria-hidden，图片念 alt）', async () => {
    const { container, unmount } = await mount(
      <div>
        <h2 id="t">区块<span aria-hidden="true">·装饰</span></h2>
        <section aria-labelledby="t" />
        <button type="button" aria-label="关闭抽屉">✕</button>
        <button type="button"><span aria-hidden="true">▶</span>播放<span className="visually-hidden">第 2 集</span></button>
        <a href="/x"><img src="/a.png" alt="海报" />作品</a>
        <label>作品名称<input type="text" defaultValue="不算名字" /></label>
      </div>,
    )
    expect(accessibleName(getByRole(container, 'region'))).toBe('区块')
    expect(getByRole(container, 'button', { name: '关闭抽屉' }).textContent).toBe('✕')
    expect(getByRole(container, 'button', { name: '播放第 2 集' })).toBeDefined()
    expect(getByRole(container, 'link', { name: '海报作品' }).getAttribute('href')).toBe('/x')
    expect(getByRole(container, 'textbox', { name: '作品名称' })).toBeDefined()
    await unmount()
  })

  it('默认跳过读屏碰不到的：hidden、aria-hidden、没打开的 dialog、收起的 details 里的内容', async () => {
    const { container, unmount } = await mount(
      <div>
        <div hidden><button type="button">藏起来的</button></div>
        <div aria-hidden="true"><button type="button">装饰的</button></div>
        <dialog><button type="button">弹窗里的</button></dialog>
        <details><summary>展开</summary><button type="button">折叠里的</button></details>
        <button type="button">看得见的</button>
      </div>,
    )
    expect(getAllByRole(container, 'button').map(accessibleName)).toEqual(['看得见的'])
    expect(getAllByRole(container, 'button', { hidden: true })).toHaveLength(5)
    expect(getByRole(container, 'group').querySelector('summary')?.textContent).toBe('展开')
    await unmount()
  })

  it('显式 role 覆盖隐含角色；heading 可按级别找；多于一个时报错', async () => {
    const { container, unmount } = await mount(
      <div>
        <button type="button" role="tab">本地</button>
        <h1>标题</h1><h3>小标题</h3>
        <p role="status">一</p><output>二</output>
      </div>,
    )
    expect(queryByRole(container, 'button')).toBeNull()
    expect(getByRole(container, 'tab', { name: '本地' })).toBeDefined()
    expect(getByRole(container, 'heading', { level: 3 }).textContent).toBe('小标题')
    expect(() => getByRole(container, 'status')).toThrow(/找到 2 个/)
    expect(getByText(container, /^二$/).tagName).toBe('OUTPUT')
    await unmount()
  })
})
