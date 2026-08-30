import { act } from 'react'
import { createRoot } from 'react-dom/client'
import type { ReactElement } from 'react'

/**
 * 极简测试渲染器（jsdom 环境用）。
 * 项目刻意不引 @testing-library/react —— 现有依赖里 react-dom + act 足够
 * 覆盖 M1 的组件/hook 测试面，少一个依赖少一份供应链。
 * 使用方文件需带 `// @vitest-environment jsdom` pragma。
 */

// react-dom 要求显式声明「当前处于 act 测试环境」，否则 act 内的更新会告警
declare global {
  // eslint 风格上更想用 const，但 React 检查的就是这个可写全局
  var IS_REACT_ACT_ENVIRONMENT: boolean | undefined
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true

export interface MountResult {
  container: HTMLElement
  rerender: (element: ReactElement) => Promise<void>
  unmount: () => Promise<void>
}

/** 挂载任意元素，等 effect 与在途微任务都稳定后返回 */
export async function mount(element: ReactElement): Promise<MountResult> {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(element)
  })
  return {
    container,
    rerender: async (next: ReactElement) => {
      await act(async () => {
        root.render(next)
      })
    },
    unmount: async () => {
      await act(async () => {
        root.unmount()
      })
      container.remove()
    },
  }
}

export interface HookHandle<T> {
  /** 每次重渲染后指向最新的 hook 返回值 */
  result: { current: T }
  unmount: () => Promise<void>
}

/** 把 hook 挂进一个探针组件里跑起来 */
export async function mountHook<T>(useHook: () => T): Promise<HookHandle<T>> {
  const result = { current: undefined as T }
  function Probe() {
    result.current = useHook()
    return null
  }
  const mounted = await mount(<Probe />)
  return { result, unmount: mounted.unmount }
}
