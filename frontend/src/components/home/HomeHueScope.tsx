import { createContext, useContext, useMemo, useState } from 'react'
import type { CSSProperties, ReactNode } from 'react'
import { pageToneVars } from '../../lib/home/tone'
import styles from './HomeHueScope.module.css'

// 发现页穿着横幅当前那一部作品的颜色。
//
// 底色、面、强调色的字符串在 TS 里拼好（lib/home/tone.ts），写在【这个】元素上，由它的后代去读。
// 没有任何颜色是用别的元素上声明的色相组装出来的 —— 那是 DESIGN.md「陷阱」一节写的静默失效。

interface HeroFocus {
  active: number
  setActive: (index: number) => void
}

const HeroFocusContext = createContext<HeroFocus>({ active: 0, setActive: () => {} })

export function useHeroFocus(): HeroFocus {
  return useContext(HeroFocusContext)
}

interface HomeHueScopeProps {
  /** 横幅每一张一个色相，按顺序；null = 中性 */
  hues: Array<number | null>
  children: ReactNode
}

/** 发现页的 <main>：整页底色与强调色跟着横幅当前那一张走 */
export function HomeHueScope({ hues, children }: HomeHueScopeProps) {
  const [active, setActive] = useState(0)
  const value = useMemo(() => ({ active, setActive }), [active])
  // 与横幅一样夹到最后一张：后台刷新后张数变少时，整页颜色与横幅显示的那一张对得上
  const vars = pageToneVars(hues[Math.min(active, hues.length - 1)] ?? null) as unknown as CSSProperties

  return (
    <HeroFocusContext.Provider value={value}>
      <main className={styles.scope} style={vars}>
        {children}
      </main>
    </HeroFocusContext.Provider>
  )
}
