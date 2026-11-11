import { useRef, useState } from 'react'
import type { CSSProperties } from 'react'
import { fetchMoreGems } from '../../lib/endpoints'
import { cardToneVars } from '../../lib/home/tone'
import { gemCard, type GemCard } from '../../lib/home/viewModels'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { CoverImage } from './CoverImage'
import { SectionHeader } from './SectionHeader'
import { RefreshIcon } from './icons'
import cards from './cards.module.css'
import section from './section.module.css'
import styles from './GemsPanel.module.css'

// 完结佳作：已完结的高分作品，一次随机一小批。
//
// 第一批随发现页一起来；「换一批」再向后端要下一批（GET /api/discover/gems，每次都是新的随机样本）。
// 两批放在叠起来的两个槽里，换一批时写进看不见的那个再翻过来，所以是错开的交叉淡入，淡出的卡片在淡出时内容不变。
// 换一批失败（或一部都没拿到）时当前这一批留在屏上 —— 屏上没有东西因此变错。

interface GemsPanelProps {
  initial: GemCard[]
}

type Vars = CSSProperties & Record<`--${string}`, string>

export function GemsPanel({ initial }: GemsPanelProps) {
  const [slots, setSlots] = useState<[GemCard[], GemCard[]]>([initial, []])
  const [front, setFront] = useState<0 | 1>(0)
  const [turns, setTurns] = useState(0)
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)

  if (initial.length === 0) return null

  const refresh = async () => {
    if (busyRef.current) return
    busyRef.current = true
    setBusy(true)
    setTurns((n) => n + 1)
    try {
      const data = await fetchMoreGems()
      if (data.items.length === 0) return
      const next = data.items.map(gemCard)
      const back: 0 | 1 = front === 0 ? 1 : 0
      setSlots((prev) => (back === 0 ? [next, prev[1]] : [prev[0], next]))
      setFront(back)
    } catch (err) {
      // 当前这一批留着；记一笔，不打扰读者
      console.warn('换一批完结佳作失败，保留当前这一批', err)
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }

  return (
    <section className={styles.section} aria-labelledby="home-gems">
      <SectionHeader
        id="home-gems"
        title="完结佳作"
        note="已完结的高分番剧 · 随机挑选"
        action={
          // aria-disabled 而不是 disabled：请求中按钮变 disabled 会把焦点甩到页面最上面（DESIGN.md「无障碍」）
          <button type="button" className={section.link} onClick={() => void refresh()} aria-disabled={busy}>
            <span className={styles.refreshIcon} style={{ transform: `rotate(${turns * 180}deg)` }} aria-hidden="true">
              <RefreshIcon />
            </span>
            换一批
          </button>
        }
      />
      <div className={styles.stage}>
        {slots.map((items, slot) => {
          const on = slot === front
          return (
            <div key={slot} className={styles.page} data-active={on} aria-hidden={!on} inert={!on}>
              {items.map((g, k) => {
                const vars = { ...(cardToneVars(g.hue) as unknown as Vars), '--stagger': `${80 + k * 60}ms` } as Vars
                return (
                  <MediaEntryLink key={`${slot}-${g.id}`} id={g.id} className={`${cards.card} ${styles.card}`} style={vars}>
                    <div className={`${cards.cover} ${styles.cover}`}>
                      <span className={cards.zoom}>
                        <CoverImage src={g.cover} className={cards.img} />
                      </span>
                      <span className={cards.rule} aria-hidden="true" />
                    </div>
                    <div className={styles.body}>
                      <span className={`${cards.title} ${cards.clamp2} ${styles.title}`}>{g.title}</span>
                      {g.native ? (
                        <span className={`${cards.clamp1} ${styles.native}`} lang="ja">
                          {g.native}
                        </span>
                      ) : null}
                      {g.meta ? <span className={`${cards.clamp1} ${styles.meta}`}>{g.meta}</span> : null}
                      {g.score ? (
                        <span className={styles.score}>
                          <span className={`${cards.mono} ${styles.scoreValue}`}>{g.score}</span>
                          <span className={styles.scoreSource}>{g.scoreSource}</span>
                        </span>
                      ) : null}
                    </div>
                  </MediaEntryLink>
                )
              })}
            </div>
          )
        })}
      </div>
    </section>
  )
}
