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

  it('页头只在不属于某个区块时才是 banner；表格、开关、搜索表单按角色找得到', async () => {
    const { container, unmount } = await mount(
      <div>
        <header>顶栏</header>
        <main>
          <header>页面自己的页头</header>
          <form role="search" aria-label="磁力搜索" />
          <button type="button" role="switch" aria-checked="true" aria-label="持续做种" />
          <table>
            <thead><tr><th>标题</th></tr></thead>
            <tbody><tr><td>第 1 集</td></tr></tbody>
          </table>
        </main>
      </div>,
    )
    expect(getByRole(container, 'banner').textContent).toBe('顶栏')
    expect(getByRole(container, 'search', { name: '磁力搜索' })).toBeDefined()
    expect(getByRole(container, 'switch', { name: '持续做种' }).getAttribute('aria-checked')).toBe('true')
    expect(getAllByRole(getByRole(container, 'table'), 'row')).toHaveLength(2)
    expect(getByRole(container, 'columnheader').textContent).toBe('标题')
    expect(getByRole(container, 'cell', { name: '第 1 集' })).toBeDefined()
    await unmount()
  })

  it('列表、导航、分组这类容器不拿内容文字当名字：没标名字就是没有名字', async () => {
    const { container, unmount } = await mount(
      <div>
        <nav><a href="/a">账号</a></nav>
        <ul><li>账号</li></ul>
        <p id="g">播放</p>
        <ul aria-labelledby="g"><li>播放器</li></ul>
        <fieldset><legend>画质档位</legend><input type="checkbox" aria-label="开" /></fieldset>
        <p role="note">登录会把网站上的会话踢下线</p>
      </div>,
    )
    expect(accessibleName(getByRole(container, 'navigation'))).toBe('')
    expect(getAllByRole(container, 'list').map(accessibleName)).toEqual(['', '播放'])
    expect(queryByRole(container, 'list', { name: /账号/ })).toBeNull()
    expect(getByRole(container, 'group', { name: '画质档位' })).toBeDefined()
    // 提示类角色照旧按内容找：测试里认的就是它念出来的那句话
    expect(getByRole(container, 'note', { name: /踢下线/ })).toBeDefined()
    await unmount()
  })
})
