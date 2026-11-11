import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { CSSProperties, KeyboardEvent } from 'react'
import { isArtworkLoaded, preloadArtwork } from '../../lib/imageCache'
import { HERO_INTERVAL_MS } from '../../lib/home/heroRotation'
import { heroStatusParts, statusText } from '../../lib/home/heroStatus'
import { weekdayTime } from '../../lib/home/time'
import { tokenDelays } from '../../lib/home/titleTokens'
import { cardToneVars, toneLadder } from '../../lib/home/tone'
import type { HeroSlide } from '../../lib/home/viewModels'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { HeroFollowButton } from './HeroFollowButton'
import { useHeroFocus } from './HomeHueScope'
import { ArrowIcon, PauseIcon, PlayIcon, StarIcon } from './icons'
import { useHeroRotation } from './useHeroRotation'
import { useHomeClock } from './useHomeClock'
import styles from './HomeHero.module.css'

// 发现页横幅：本季前五部，一次一部在焦点上。
//
// 构图：当前那张封面模糊成背后的氛围光，横幅图从右边淡入，左边一张小封面和几行字。
//
// 每 8 秒换下一张；鼠标在上面、键盘焦点在里面、按了暂停、标签页在后台、横幅滚出视野时停住；
// 减弱动态效果时从不自己切（WCAG 2.2.2 —— 规则在 lib/home/heroRotation.ts，接线在 useHeroRotation）。
// 五根短条各穿自己作品的颜色，手动切换；当前那根在 8 秒里填满，停住时停下。
//
// 五张一直挂着、叠在一起，切换只是 CSS 过渡 —— 横幅图交叉淡入、封面弹出、标题逐块去模糊、文字行依次滑入。
// 没有东西重新挂载，就没有东西重新请求、没有东西闪。整页颜色经 HomeHueScope 跟着走。
//
// 横幅图是这一页最重的图：只在一张到了焦点上、被悬停或聚焦到、或快轮到它时才开始加载，第一屏只拉一张。
// 而且只有加载完的图才会淡入：新的一张还没到时，上一张留在原处，不露出底色（WebKit 里懒加载要等到
// 快露出来才开始，直接挂上去就是一次「黑一下」）。

interface HomeHeroProps {
  slides: HeroSlide[]
}

type Vars = CSSProperties & Record<`--${string}`, string>

export function HomeHero({ slides }: HomeHeroProps) {
  const { active, setActive } = useHeroFocus()
  const nowMs = useHomeClock()
  // 第一次切换（手动或到点）时翻过来：进场动画只属于第一次画出来，播报区在那之前也没什么可说
  const [switched, setSwitched] = useState(false)
  const [warm, setWarm] = useState<ReadonlySet<number>>(() => new Set([0]))
  // 加载完（或失败）的横幅图；挂载时已经在内存里的直接算好
  const [settled, setSettled] = useState<ReadonlyMap<string, boolean>>(
    () => new Map(slides.flatMap((s) => (s.banner && isArtworkLoaded(s.banner) ? [[s.banner, true] as const] : []))),
  )
  const barRefs = useRef<Array<HTMLButtonElement | null>>([])
  const heroRef = useRef<HTMLElement | null>(null)

  const count = slides.length
  const current = count === 0 ? 0 : Math.min(active, count - 1)

  // 后台刷新换了数据（离线时的旧一份过期后重取）、前五部的顺序变了：焦点跟着原来那一部走，
  // 读者眼前这一张不会被换成另一部（它还在前五里的话）
  const slideIds = slides.map((s) => s.id).join(',')
  const shown = useRef<{ ids: string; id: number | null }>({ ids: slideIds, id: null })
  useLayoutEffect(() => {
    const last = shown.current
    if (last.ids !== slideIds && last.id !== null) {
      // 这一组作品变了：对齐到上一次显示的那一部
      const at = slides.findIndex((s) => s.id === last.id)
      shown.current = { ids: slideIds, id: last.id }
      if (at >= 0 && at !== current) {
        setActive(at)
        return
      }
    }
    // 平常的切换：记下现在显示的是哪一部
    shown.current = { ids: slideIds, id: slides[current]?.id ?? null }
  })

  const warmUp = (index: number) => {
    setWarm((prev) => (prev.has(index) ? prev : new Set(prev).add(index)))
  }

  const switchTo = (index: number) => {
    warmUp(index)
    if (index === current) return
    setActive(index)
    setSwitched(true)
  }

  // 要在下面的提前返回之前调用：hook 不能排在它后面
  const rotation = useHeroRotation({ rootRef: heroRef, count, current, onPreload: warmUp, onAdvance: switchTo })

  // 预热过的横幅图在后台加载、解码完，才算「能显示」
  const warmBanners = [...warm].map((i) => slides[i]?.banner).filter((src): src is string => !!src)
  const pending = warmBanners.filter((src) => !settled.has(src)).join('\n')
  useEffect(() => {
    if (pending === '') return
    let cancelled = false
    for (const src of pending.split('\n')) {
      void preloadArtwork(src).then((ok) => {
        if (!cancelled) setSettled((prev) => (prev.has(src) ? prev : new Map(prev).set(src, ok)))
      })
    }
    return () => {
      cancelled = true
    }
  }, [pending])

  // 横幅图显示哪一张：当前那张的图到了（或它没有图）就是它；还没到就留着上一张
  const currentBanner = slides[current]?.banner ?? null
  const currentReady = currentBanner === null || settled.has(currentBanner)
  const [shownBanner, setShownBanner] = useState(current)
  if (currentReady && shownBanner !== current) setShownBanner(current)

  if (count === 0) return null

  // 手动切换；新的一张的 8 秒也从头算（倒计时按这一张记，见 useHeroRotation）
  const pick = switchTo

  // 方向键在短条之间移动，与任何分段控件一样。只处理这几个键 —— 全吞掉会困住 Tab
  const onBarsKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const next =
      event.key === 'ArrowRight' || event.key === 'ArrowDown'
        ? (current + 1) % count
        : event.key === 'ArrowLeft' || event.key === 'ArrowUp'
          ? (current - 1 + count) % count
          : event.key === 'Home'
            ? 0
            : event.key === 'End'
              ? count - 1
              : null
    if (next === null) return
    event.preventDefault()
    pick(next)
    barRefs.current[next]?.focus()
  }

  return (
    <section
      ref={heroRef}
      className={styles.hero}
      aria-label="本季焦点"
      data-entered={switched ? 'true' : 'false'}
      data-rotates={rotation.rotates ? 'true' : 'false'}
      data-held={rotation.held ? 'true' : 'false'}
      style={{ '--hero-interval': `${HERO_INTERVAL_MS}ms` } as Vars}
      {...rotation.rootHandlers}
    >
      <div className={styles.glow} aria-hidden="true">
        <div className={styles.glowInner}>
          {slides.map((s, i) =>
            s.cover ? <img key={s.id} src={s.cover} alt="" className={styles.glowImg} data-active={i === current} loading="eager" decoding="async" /> : null,
          )}
        </div>
      </div>

      {slides.map((s, i) => {
        const ready = s.banner !== null && settled.get(s.banner) === true
        return (
          <div key={s.id} className={styles.banner} data-active={i === shownBanner} aria-hidden="true">
            {ready ? (
              <div className={styles.bannerIn}>
                <img src={s.banner ?? undefined} alt="" className={styles.bannerImg} data-cover={s.hasBanner ? undefined : 'true'} decoding="async" />
              </div>
            ) : null}
          </div>
        )
      })}

      <div className={styles.topShade} aria-hidden="true" />
      <div className={styles.scrim} aria-hidden="true" />

      <div className={styles.content}>
        <div className={styles.eyebrow}>
          <span className={styles.eyebrowLabel} aria-hidden="true">本季焦点</span>
          <div className={styles.bars} role="group" aria-label="本季焦点" onKeyDown={onBarsKeyDown}>
            {slides.map((s, i) => {
              const tone = toneLadder(s.hue)
              const vars: Vars = { '--bar-on': tone.text, '--bar-off': tone.line }
              return (
                <button
                  key={s.id}
                  ref={(el) => {
                    barRefs.current[i] = el
                  }}
                  type="button"
                  className={styles.bar}
                  style={vars}
                  aria-pressed={i === current}
                  aria-label={`本季焦点 ${i + 1}：${s.title}`}
                  onClick={() => pick(i)}
                  onPointerEnter={() => warmUp(i)}
                  onFocus={() => warmUp(i)}
                >
                  <span className={styles.barFill}>
                    {/* 倒计时，只在当前那根上。8 秒不经切换就从头算时（标签页回来了）换 key，填充跟着从头来 */}
                    {rotation.rotates && i === current ? <span key={rotation.restarts} className={styles.barProgress} /> : null}
                  </span>
                  <span className={styles.barTip} aria-hidden="true">
                    {s.title}
                  </span>
                </button>
              )
            })}
          </div>
          {/* WCAG 2.2.2 的暂停控件。只在横幅会轮换时出现（减弱动态效果时没有可暂停的）；排在短条后面，
              出现时不挪动任何东西。名字说的是按下去会做什么 */}
          {rotation.rotates ? (
            <button type="button" className={styles.pause} onClick={rotation.togglePause}>
              {rotation.paused ? <PlayIcon size={12} /> : <PauseIcon size={12} />}
              <span className={styles.srOnly}>{rotation.paused ? '继续自动切换' : '暂停自动切换'}</span>
              <span className={styles.barTip} aria-hidden="true">
                {rotation.paused ? '继续自动切换' : '暂停自动切换'}
              </span>
            </button>
          ) : null}
        </div>

        <div className={styles.stage}>
          <div className={styles.covers} aria-hidden="true">
            {slides.map((s, i) => {
              const vars = cardToneVars(s.hue) as unknown as Vars
              return s.cover ? (
                <img key={s.id} src={s.cover} alt="" className={styles.cover} style={vars} data-active={i === current} loading="eager" decoding="async" />
              ) : (
                <span key={s.id} className={styles.cover} style={vars} data-active={i === current} />
              )
            })}
          </div>

          {slides.map((s, i) => (
            <HeroSlideText key={s.id} slide={s} active={i === current} nowMs={nowMs} />
          ))}
        </div>
      </div>

      {/* 自己轮换时不念 —— 否则读屏每 8 秒报一张。手动切换时横幅是停住的（指针或焦点在上面），会念 */}
      <p className={styles.srOnly} aria-live={rotation.held ? 'polite' : 'off'}>
        {switched ? `${current + 1} / ${count}：${slides[current].title}` : ''}
      </p>
    </section>
  )
}

function HeroSlideText({ slide: s, active, nowMs }: { slide: HeroSlide; active: boolean; nowMs: number }) {
  const tone = toneLadder(s.hue)
  const vars: Vars = { '--tone': tone.text, '--tone-fill': tone.fill, '--on-solid': tone.onSolid }
  const parts = heroStatusParts({ status: s.status, statusLabel: s.statusLabel, episodes: s.episodes, airings: s.airings }, nowMs, (ms) => weekdayTime(ms))
  const longStatus = statusText(parts)
  const shortStatus = statusText(parts, { short: true })
  return (
    <article className={styles.slide} style={vars} data-active={active} aria-hidden={!active} inert={!active}>
      <div className={styles.head}>
        <h2 className={styles.title}>
          <span className={styles.srOnly}>{s.title}</span>
          <span aria-hidden="true">
            {s.tokens.map((token, k) => {
              const d = tokenDelays(k)
              const tv: Vars = { '--td': `${d.switchMs}ms`, '--ld': `${d.entryMs}ms` }
              return (
                <span key={k} className={styles.tok} style={tv}>
                  {token}
                </span>
              )
            })}
          </span>
        </h2>
        {s.genres.length > 0 ? (
          <p className={`${styles.genres} ${styles.line} ${styles.l1}`}>
            {s.genres.map((g) => (
              <span key={g}>{g}</span>
            ))}
          </p>
        ) : null}
        {s.score || longStatus ? (
          <div className={`${styles.meta} ${styles.line} ${styles.l2}`}>
            {s.score ? (
              <span className={styles.score}>
                <StarIcon />
                <span className={styles.srOnly}>评分 </span>
                <span className={styles.mono}>{s.score}</span>
              </span>
            ) : null}
            {longStatus ? (
              <span className={styles.status}>
                <span className={styles.dot} data-live={parts.live} aria-hidden="true" />
                <span className={styles.statusLong}>{longStatus}</span>
                <span className={styles.statusShort}>{shortStatus}</span>
              </span>
            ) : null}
          </div>
        ) : null}
      </div>
      {s.synopsis ? <p className={`${styles.synopsis} ${styles.line} ${styles.l3}`}>{s.synopsis}</p> : null}
      <div className={`${styles.actions} ${styles.line} ${styles.l4}`}>
        <MediaEntryLink id={s.id} className={styles.solid} aria-label={`查看详情：${s.title}`}>
          查看详情
          <ArrowIcon className={styles.arrow} />
        </MediaEntryLink>
        <HeroFollowButton id={s.id} title={s.title} episodes={s.episodes} className={styles.glass} />
      </div>
    </article>
  )
}
