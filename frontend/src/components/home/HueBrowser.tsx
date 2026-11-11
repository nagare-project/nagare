import { useEffect, useRef, useState } from 'react'
import type { CSSProperties } from 'react'
import { nextFamily, type HueFamilyKey } from '../../lib/home/hueFamilies'
import { toneLadder } from '../../lib/home/tone'
import type { HueCard } from '../../lib/home/viewModels'
import { HueRail } from './HueRail'
import { SectionHeader } from './SectionHeader'
import section from './section.module.css'
import styles from './HueBrowser.module.css'

// 按色调逛：本季作品按封面主色分组，一次只看一组，这一段不会变成一面海报墙。
//
// 分组由页面拼好（lib/home/hueFamilies.ts），名字也在里面。只有选哪一组是组件自己的状态。
// 每一组是一条横排，放着这一组的全部作品（HueRail.tsx）；所有面板都挂着，切换是错开的交叉淡入，与横幅一样。
// 一组的横排到头时提供下一组。
//
// 一组只有打开过（或指针、焦点到过它的胶囊上）才加载封面。叠在一起、看不见的面板与看得见的那一块排版一样，
// 不这样的话每一组都会加载它前几屏的封面 —— 七排没人看得见的图，跟页面其余部分抢图片与连接。

export interface HueFamilyView {
  key: HueFamilyKey
  label: string
  /** 这一组的代表角度：色点与选中胶囊的颜色 */
  hue: number
  items: HueCard[]
}

interface HueBrowserProps {
  /** 本季一共有色相的几部 */
  total: number
  families: HueFamilyView[]
  defaultKey: HueFamilyKey
}

type Vars = CSSProperties & Record<`--${string}`, string>

/**
 * 手机上胶囊横向滚动，一个胶囊可能在屏外。把它带进这一行的视野 —— 滚的是这一行自己，
 * 不用 scrollIntoView，那会把页面也滚了。
 */
function revealPill(row: HTMLElement, pill: HTMLElement) {
  if (row.scrollWidth <= row.clientWidth) return
  const start = pill.offsetLeft
  const end = start + pill.offsetWidth
  if (start >= row.scrollLeft && end <= row.scrollLeft + row.clientWidth) return
  row.scrollLeft = start - 20
}

export function HueBrowser({ total, families, defaultKey }: HueBrowserProps) {
  const [selected, setSelected] = useState<HueFamilyKey>(defaultKey)
  const [opened, setOpened] = useState<ReadonlySet<HueFamilyKey>>(() => new Set([defaultKey]))
  const pillsRef = useRef<HTMLDivElement | null>(null)

  const pillOf = (key: HueFamilyKey) => pillsRef.current?.querySelector<HTMLElement>(`[data-family="${key}"]`) ?? null

  // 默认那一组（最大的）在手机的胶囊行里可能在屏外
  useEffect(() => {
    const row = pillsRef.current
    const pill = row?.querySelector<HTMLElement>('[aria-pressed="true"]')
    if (row && pill) revealPill(row, pill)
  }, [])

  // 指针移到、或键盘聚焦到一个胶囊上：那一组的封面先开始加载（与横幅的短条同一个做法），点下去时多半已经到了
  const warmUp = (key: HueFamilyKey) => {
    setOpened((prev) => (prev.has(key) ? prev : new Set([...prev, key])))
  }

  const show = (key: HueFamilyKey) => {
    setSelected(key)
    warmUp(key)
  }

  // 横排末尾去下一组。按下的那个按钮跟着它的面板变成 inert，所以焦点交给新那一组的胶囊，不掉回页面
  const goTo = (key: HueFamilyKey) => {
    show(key)
    const row = pillsRef.current
    const pill = pillOf(key)
    if (!row || !pill) return
    revealPill(row, pill)
    pill.focus({ preventScroll: true })
  }

  if (families.length === 0) return null

  const byKey = new Map(families.map((f) => [f.key, f]))
  // 后台刷新后选中的那一组没有了：退回默认那一组（不留一个空舞台）
  const current = byKey.has(selected) ? selected : defaultKey

  return (
    <section className={section.bleed} aria-labelledby="home-hue">
      <SectionHeader id="home-hue" title="按色调逛" note={`本季 ${total} 部 · 按封面主色分组`} />
      <div ref={pillsRef} className={styles.pills} role="group" aria-label="按色调筛选">
        {families.map((f) => {
          const tone = toneLadder(f.hue)
          const vars: Vars = { '--pill-tone': tone.text, '--pill-fill': tone.fill, '--pill-line': tone.line }
          return (
            <button
              key={f.key}
              type="button"
              className={styles.pill}
              style={vars}
              data-family={f.key}
              aria-pressed={f.key === current}
              onClick={() => show(f.key)}
              onPointerEnter={() => warmUp(f.key)}
              onFocus={() => warmUp(f.key)}
            >
              <span className={styles.dot} aria-hidden="true" />
              {f.label}
              <span className={styles.count}>{`${f.items.length} 部`}</span>
            </button>
          )
        })}
      </div>
      <div className={styles.stage}>
        {families.map((f) => {
          const nextKey = nextFamily(families, f.key)
          const next = nextKey ? (byKey.get(nextKey) ?? null) : null
          return (
            <HueRail
              key={f.key}
              family={f}
              active={f.key === current}
              loadCovers={opened.has(f.key) || f.key === current}
              next={next}
              onNext={() => {
                if (nextKey) goTo(nextKey)
              }}
            />
          )
        })}
      </div>
    </section>
  )
}
