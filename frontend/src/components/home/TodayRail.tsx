import { Fragment, useEffect, useRef } from 'react'
import type { CSSProperties } from 'react'
import { nowScrollLeft } from '../../lib/home/railScroll'
import { hhmm } from '../../lib/home/time'
import { slotToday, type Slot } from '../../lib/home/todaySlots'
import { cardToneVars } from '../../lib/home/tone'
import type { TodayCard } from '../../lib/home/viewModels'
import { RouteLink } from '../catalog/RouteLink'
import { ArtworkLoadingContext } from '../media/artworkLoading'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { CoverImage } from './CoverImage'
import { SectionHeader } from './SectionHeader'
import { CheckIcon, ChevronIcon } from './icons'
import { useHomeClock } from './useHomeClock'
import { gutterOf, railGeometry, useRailDrag, useRailEntrance, useRailLoadCount, useRailMotion, useRailSlider } from './useRail'
import cards from './cards.module.css'
import railSlider from './railSlider.module.css'
import section from './section.module.css'
import styles from './TodayRail.module.css'

// 今日更新：今天的各集横成一排，中间一个「现在」标记 —— 左边是已播的（带「已播」），右边是还没播的，
// 一小时内要播的倒数分钟、闪一闪。一排的末尾说今天的就这些、明天有几部、完整放送表在哪。
//
// 哪些集算「今天」由页面按本机的日子算好传进来；页面开着时变的只是每一集在「现在」的哪一边，
// 那来自每分钟走一格的时钟（useHomeClock）。
//
// 当天的每一部在任何宽度下都是自己的一张卡。手机上曾经把已播的一半折进一张卡，页面开着时刚播出的那张
// 掉进折叠里 —— 那部作品从这一排消失了。
//
// 移动：触摸原生横滑（手机上一张张地停）。桌面鼠标滚轮做不到（它滚页面），所以用手拖（拖动不会打开起点那张卡；
// 快速拖动会接着滑、停在卡上）。每个宽度下这一排下面都有一根细滑块：滑块与屏上所占比例一样宽，可以拖、点轨道，
// 或者用它后面的 range 输入（键盘）。算术在 lib/home/railScroll.ts，接线在 useRail.ts（与按色调逛共用）。

const RAIL_ID = 'home-today-rail'

/** 今天的下一天，给一排的末尾用 */
export interface TodayRailTomorrow {
  /** 「周日 10月11日」 */
  label: string
  /** 那天有几集；0 就只说是哪天 */
  count: number
}

interface TodayRailProps {
  items: TodayCard[]
  /** 这一天，写好的（「周四 9月24日」） */
  dayLabel: string
  tomorrow: TodayRailTomorrow | null
}

export function TodayRail({ items, dayLabel, tomorrow }: TodayRailProps) {
  const now = hhmm(useHomeClock())
  return (
    <section className={section.bleed} aria-labelledby="home-today">
      <SectionHeader
        id="home-today"
        title="今日更新"
        count={items.length}
        note={[dayLabel, `现在 ${now}`].filter(Boolean).join(' · ')}
        link={{ to: '/schedule', label: '完整放送表' }}
        phoneHides={['note']}
      />
      {items.length === 0 ? (
        <p className={`${section.empty} ${styles.empty}`}>今日暂无更新</p>
      ) : (
        <TodayRailBody items={items} tomorrow={tomorrow} />
      )}
    </section>
  )
}

/** 这一排本身。挂监听的 hook 都在这里：它挂载时横排元素一定在，今天的番晚到（或过了零点才有）时拖动、滑块、停在「现在」都接得上 */
function TodayRailBody({ items, tomorrow }: { items: TodayCard[]; tomorrow: TodayRailTomorrow | null }) {
  const nowMs = useHomeClock()
  const railRef = useRef<HTMLDivElement | null>(null)
  const thumbRef = useRef<HTMLSpanElement | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const motion = useRailMotion(railRef)
  const drag = useRailDrag({ onPress: motion.stop, onRelease: motion.fling })
  const slider = useRailSlider(railRef, { thumbRef, inputRef }, motion, (first, last, total) => `第 ${first}–${last} 部，共 ${total} 部`)
  const entering = useRailEntrance(railRef)
  const loadCount = useRailLoadCount(railRef)

  const { slots, nowIndex } = slotToday(items, nowMs)
  const now = hhmm(nowMs)

  // 一打开就停在「现在」：晚上十一点的读者不该先滚过十五集播完的才找到下一集。滚的是这一排自己 ——
  // 不用 scrollIntoView，那会把页面也滚了。瞬间到位：这是一排开始的地方，不是一次移动。
  // 每次挂载只一次：effect 再跑一次（开发时的热更新）不能把读者已经挪走的一排拽回来
  const opened = useRef(false)
  useEffect(() => {
    const rail = railRef.current
    const marker = rail?.querySelector<HTMLElement>('[data-now]')
    if (opened.current || !rail || !marker) return
    opened.current = true
    const prev = marker.previousElementSibling as HTMLElement | null
    const next = marker.nextElementSibling as HTMLElement | null
    const left = nowScrollLeft(
      railGeometry(rail),
      {
        markerStart: marker.offsetLeft,
        prevStart: prev ? prev.offsetLeft : null,
        nextEnd: next ? next.offsetLeft + next.offsetWidth : marker.offsetLeft + marker.offsetWidth,
      },
      gutterOf(rail),
    )
    if (left !== null) rail.scrollLeft = left
    // 只在到达时；之后滚动位置归读者
  }, [])

  const card = (slot: Slot<TodayCard>, index: number) => {
    const it = slot.item
    const tone = cardToneVars(it.hue) as unknown as CSSProperties
    return (
      <ArtworkLoadingContext.Provider key={it.key} value={index < loadCount ? 'eager' : 'lazy'}>
        <MediaEntryLink id={it.id} className={`${cards.card} ${styles.card}`} style={tone} data-card="" data-state={slot.state}>
          <div className={`${cards.cover} ${styles.cover}`}>
            <span className={`${cards.zoom} ${styles.zoom}`}>
              <CoverImage src={it.cover} className={cards.img} />
            </span>
            <span className={styles.fade} aria-hidden="true" />
            {slot.state === 'aired' ? (
              <span className={styles.badge}>
                <CheckIcon size={12} />
                已播
              </span>
            ) : slot.state === 'soon' ? (
              <span className={`${styles.badge} ${styles.soon}`}>
                <span className={styles.ping} aria-hidden="true" />
                {`${slot.minutesUntil ?? 1} 分钟后`}
              </span>
            ) : null}
            <span className={styles.when}>
              <time dateTime={new Date(it.at).toISOString()}>{hhmm(it.at)}</time>
              {` · 第 ${it.ep} 集`}
            </span>
            <span className={cards.rule} aria-hidden="true" />
          </div>
          <span className={`${cards.title} ${cards.clamp2} ${styles.title}`}>{it.title}</span>
        </MediaEntryLink>
      </ArtworkLoadingContext.Provider>
    )
  }

  const marker = (
    <div key="now" className={styles.now} data-now="" aria-hidden="true">
      <span className={styles.nowLabel}>现在</span>
      <span className={styles.nowTime}>{now}</span>
      <span className={styles.nowDot} />
      <span className={styles.nowLine} />
    </div>
  )

  const tomorrowLine = !tomorrow ? null : tomorrow.count > 0 ? `明天 · ${tomorrow.label} · ${tomorrow.count} 部` : `明天 · ${tomorrow.label}`

  // 被手拖着或在惯性里：不往卡上吸，指针下滑过的卡不抬起
  const moving = drag.dragging || slider.dragging || motion.flinging

  return (
    <div className={styles.frame}>
      <div
        ref={railRef}
        id={RAIL_ID}
        className={styles.rail}
        data-dragging={drag.dragging ? 'true' : undefined}
        data-moving={moving ? 'true' : undefined}
        data-enter={entering ? 'run' : undefined}
        {...drag.handlers}
      >
        {slots.map((slot, i) => (
          <Fragment key={slot.item.key}>
            {i === nowIndex ? marker : null}
            {card(slot, i)}
          </Fragment>
        ))}
        {nowIndex === slots.length ? marker : null}
        <div className={styles.end}>
          <span className={styles.endTitle}>今天的番就这些</span>
          {tomorrowLine ? <span className={styles.endNext}>{tomorrowLine}</span> : null}
          <RouteLink to="/schedule" className={styles.endLink}>
            看完整放送表
            <ChevronIcon size={14} />
          </RouteLink>
        </div>
      </div>
      {/* 键盘与辅助技术沿这一排走的途径；后面的滑块是它的图像，也给指针用。值、最大值、念的值由 useRailSlider 跟这一排同步 */}
      <input
        ref={inputRef}
        type="range"
        className={railSlider.scrub}
        aria-label="横向滚动今日更新"
        aria-controls={RAIL_ID}
        min={0}
        step={1}
        defaultValue={0}
        disabled={!slider.overflow}
        {...slider.inputHandlers}
      />
      <div
        className={railSlider.slider}
        data-overflow={slider.overflow ? 'true' : 'false'}
        data-active={slider.active ? 'true' : undefined}
        data-dragging={slider.dragging ? 'true' : undefined}
        aria-hidden="true"
        {...slider.sliderHandlers}
      >
        <span className={railSlider.track} />
        <span ref={thumbRef} className={railSlider.thumb} />
      </div>
    </div>
  )
}
