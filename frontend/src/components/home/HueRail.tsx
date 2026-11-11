import { useRef } from 'react'
import type { CSSProperties } from 'react'
import { cardToneVars, toneLadder } from '../../lib/home/tone'
import type { HueCard } from '../../lib/home/viewModels'
import { ArtworkLoadingContext } from '../media/artworkLoading'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { CoverImage } from './CoverImage'
import type { HueFamilyView } from './HueBrowser'
import { ChevronIcon } from './icons'
import { useRailDrag, useRailLoadCount, useRailMotion, useRailSlider } from './useRail'
import cards from './cards.module.css'
import railSlider from './railSlider.module.css'
import styles from './HueBrowser.module.css'

// 按色调逛的一组：这一组的每一部作品排成一条横排，然后一块面板说下一组是什么。下面是与今日更新同样的滑块，
// 涂这一组自己的颜色。
//
// 横排是今日更新那一种（useRail.ts）：触摸原生滑，鼠标拖（快速拖动接着滑、停在卡上），滑块可以拖、点轨道，
// 或者用后面的 range 输入（键盘）。每一组各有自己的横排，去别的颜色再回来，横排还在原来的地方。

interface HueRailProps {
  family: HueFamilyView
  /** 正在显示的一组；其余的挂着、隐藏、inert，给交叉淡入用 */
  active: boolean
  /** 这一组显示过：可以加载封面。在那之前每张卡只有作品色底 */
  loadCovers: boolean
  /** 一排的末尾把读者送去哪；没有别的组时为 null */
  next: HueFamilyView | null
  onNext: () => void
}

type Vars = CSSProperties & Record<`--${string}`, string>

/** 一组出现时依次浮起的卡片数；其余的跟最后一张一起浮起 */
const STAGGERED = 8

export function hueRailId(family: HueFamilyView): string {
  return `home-hue-${family.key}`
}

export function HueRail({ family, active, loadCovers, next, onNext }: HueRailProps) {
  const railRef = useRef<HTMLDivElement | null>(null)
  const thumbRef = useRef<HTMLSpanElement | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const motion = useRailMotion(railRef)
  const drag = useRailDrag({ onPress: motion.stop, onRelease: motion.fling })
  const slider = useRailSlider(railRef, { thumbRef, inputRef }, motion, (first, last, total) => `第 ${first}–${last} 部，共 ${total} 部`)
  const loadCount = useRailLoadCount(railRef, loadCovers)

  // 被手拖着或在惯性里：不往卡上吸，指针下滑过的卡不抬起
  const moving = drag.dragging || slider.dragging || motion.flinging
  const tone = toneLadder(family.hue)
  const panelVars: Vars = { '--slider-line': tone.line, '--slider-tone': tone.text }
  const railId = hueRailId(family)

  return (
    <div className={styles.panel} style={panelVars} data-active={active} aria-hidden={!active} inert={!active}>
      <div
        ref={railRef}
        id={railId}
        className={styles.rail}
        data-dragging={drag.dragging ? 'true' : undefined}
        data-moving={moving ? 'true' : undefined}
        {...drag.handlers}
      >
        {family.items.map((it, k) => (
          <ArtworkLoadingContext.Provider key={it.id} value={k < loadCount ? 'eager' : 'lazy'}>
            <HueRailCard item={it} order={k} loadCover={loadCovers} />
          </ArtworkLoadingContext.Provider>
        ))}
        {next ? (
          <div className={styles.end}>
            <span className={styles.endTitle}>{`${family.label}就这些`}</span>
            <span className={styles.endNext}>{`下一组 · ${next.label} ${next.items.length} 部`}</span>
            <button type="button" className={styles.endLink} style={{ color: toneLadder(next.hue).text }} onClick={onNext}>
              {`看${next.label}`}
              <ChevronIcon size={14} />
            </button>
          </div>
        ) : null}
      </div>
      {/* 键盘与辅助技术沿这一排走的途径；后面的滑块是它的图像，也给指针用。由 useRailSlider 跟这一排同步 */}
      <input
        ref={inputRef}
        type="range"
        className={railSlider.scrub}
        aria-label={`横向滚动${family.label}`}
        aria-controls={railId}
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

function HueRailCard({ item: it, order, loadCover }: { item: HueCard; order: number; loadCover: boolean }) {
  const vars = {
    ...(cardToneVars(it.hue) as unknown as Vars),
    '--stagger': `${60 + Math.min(order, STAGGERED - 1) * 60}ms`,
  } as Vars
  return (
    <MediaEntryLink id={it.id} className={`${cards.card} ${styles.card}`} style={vars} data-card="">
      <div className={`${cards.cover} ${styles.cover}`}>
        <span className={`${cards.zoom} ${styles.zoom}`}>
          <CoverImage src={loadCover ? it.cover : null} className={cards.img} />
        </span>
        <span className={styles.band} aria-hidden="true" />
      </div>
      <div className={styles.body}>
        <span className={`${cards.title} ${cards.clamp2} ${styles.title}`}>{it.title}</span>
        <span className={cards.meta}>
          {it.score ? <span className={cards.score}>{it.score}</span> : null}
          <span className={cards.clamp1}>{it.meta}</span>
        </span>
      </div>
    </MediaEntryLink>
  )
}
