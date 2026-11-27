import { Fragment, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import { RouteLink } from '../catalog/RouteLink'
import { adjacentSeason, currentSeason, SEASON_LABEL } from '../catalog/season'
import { CoverImage } from '../home/CoverImage'
import hueScope from '../home/HomeHueScope.module.css'
import { MediaEntryLink } from '../media/MediaEntryLink'
import { Icon } from '../ui/Icon'
import { hhmm } from '../../lib/home/time'
import { cardToneVars, pageToneVars } from '../../lib/home/tone'
import type { Slot } from '../../lib/home/todaySlots'
import {
  barHeight,
  dayTimeline,
  followingThisWeek,
  nextTabIndex,
  revealScrollLeft,
  rowFollowState,
  whenLabel,
  type FollowEntry,
  type ScheduleDayView,
  type ScheduleItemView,
} from '../../lib/schedule/board'
import board from './ScheduleBoard.module.css'
import panel from './DayPanel.module.css'

const REVEAL_PAD_PX = 20
const MAX_FOLLOW_ROWS = 8
const tabId = (i: number) => `schedule-tab-${i}`
const panelId = (i: number) => `schedule-panel-${i}`

interface ScheduleBoardProps {
  days: ScheduleDayView[]
  nowMs: number
  /** anilistId → 已看到第几集；未登录时为 null */
  progress: Readonly<Record<number, number>> | null
  freshnessNode?: ReactNode
}

function followText(e: FollowEntry<ScheduleItemView>): string {
  switch (e.state) {
    case 'unwatched':
      return '未看'
    case 'watched':
      return '已看'
    case 'behind':
      return `落后 ${e.behind} 集`
    case 'upcoming':
      return '待更新'
  }
}

function slotStatusText(slot: Slot<ScheduleItemView>): string {
  if (slot.state === 'aired') return '已播'
  if (slot.state === 'soon') return `${slot.minutesUntil ?? 1} 分钟后`
  return '待播出'
}

function DayPanelView({
  day,
  index,
  active,
  nowMs,
  progress,
}: {
  day: ScheduleDayView
  index: number
  active: boolean
  nowMs: number
  progress: Readonly<Record<number, number>> | null
}) {
  const { groups, nowIndex } = dayTimeline(day.items, nowMs)

  const nowRule = (
    <li key="now" className={panel.now} aria-hidden="true">
      <span className={panel.nowDot} />
      <span className={panel.nowLabel}>现在 {hhmm(nowMs)}</span>
      <span className={panel.nowLine} />
    </li>
  )

  return (
    <div
      role="tabpanel"
      id={panelId(index)}
      aria-labelledby={tabId(index)}
      tabIndex={0}
      hidden={!active}
      className={panel.panel}
    >
      {day.items.length === 0 ? (
        <p className={panel.empty}>这一天暂时没有排期更新。</p>
      ) : (
        <ol className={panel.groups}>
          {groups.map((g, gi) => {
            const upNext = day.isToday && gi === nowIndex
            return (
              <Fragment key={g.at}>
                {upNext ? nowRule : null}
                <li className={panel.slot} data-aired={g.aired} data-next={upNext}>
                  <div className={panel.when}>
                    <span className={panel.time}>{hhmm(g.at)}</span>
                    {g.aired ? (
                      <span className={panel.note}>已播</span>
                    ) : upNext ? (
                      <span className={panel.note}>即将播出</span>
                    ) : null}
                  </div>
                  <ul className={panel.rows}>
                    {g.slots.map((slot) => {
                      const it = slot.item
                      const mine = rowFollowState(it.ep, progress?.[it.id], slot.state === 'aired')
                      return (
                        <li key={it.key}>
                          <MediaEntryLink
                            id={it.id}
                            className={panel.row}
                            style={cardToneVars(it.hue) as unknown as CSSProperties}
                            data-state={slot.state}
                          >
                            <span className={panel.cover}>
                              <span className={panel.zoom}>
                                <CoverImage src={it.cover} className={panel.img} eager={active && gi < 4} />
                              </span>
                            </span>
                            <span className={panel.text}>
                              <span className={panel.title}>{it.title}</span>
                              <span className={panel.meta}>{it.meta}</span>
                            </span>
                            <span className={panel.state}>
                              {(mine || it.inLibrary) ? (
                                <span className={panel.badges}>
                                  {mine ? (
                                    <span className={panel.mine}>
                                      <span className={panel.mineDot} aria-hidden="true" />
                                      {mine === 'unwatched' ? '未看' : '在追'}
                                    </span>
                                  ) : null}
                                  {it.inLibrary ? <span className={panel.libBadge}>库中</span> : null}
                                </span>
                              ) : null}
                              <span className={panel.status}>
                                <span className={panel.dot} aria-hidden="true" />
                                {slotStatusText(slot)}
                              </span>
                            </span>
                            {it.score ? (
                              <span className={panel.score} aria-label={`评分 ${it.score}`}>
                                <Icon name="star" size={12} />
                                <span className={panel.scoreNum}>{it.score}</span>
                              </span>
                            ) : null}
                          </MediaEntryLink>
                        </li>
                      )
                    })}
                  </ul>
                </li>
              </Fragment>
            )
          })}
          {day.isToday && nowIndex === groups.length ? nowRule : null}
        </ol>
      )}
    </div>
  )
}

/**
 * 放送表看板（对齐网站 ScheduleBoard）：
 * 顶部标题 + 7 日可切换标签 + 左栏按时刻分组的时间线 + 右栏本周分布柱状图 / 我追的·本周 / 下一季入口。
 * 页面底色随当前选中日期的代表色平滑切换。
 */
export function ScheduleBoard({ days, nowMs, progress, freshnessNode }: ScheduleBoardProps) {
  const [selected, setSelected] = useState(0)
  const rowRef = useRef<HTMLDivElement | null>(null)
  const tabRefs = useRef<Array<HTMLButtonElement | null>>([])

  const cur = currentSeason(new Date(nowMs))
  const next = adjacentSeason(cur.season, cur.year, 1)
  const totalCount = days.reduce((sum, d) => sum + d.items.length, 0)
  const maxDayCount = Math.max(0, ...days.map((d) => d.items.length))
  const hue = days[selected]?.hue ?? null
  const followEntries = progress ? followingThisWeek(days, progress, nowMs) : null

  function select(index: number, viaKeyboard = false) {
    setSelected(index)
    const row = rowRef.current
    const tab = tabRefs.current[index]
    if (!row || !tab) return
    if (viaKeyboard) tab.focus({ preventScroll: true })
    const left = revealScrollLeft(
      {
        viewLeft: row.scrollLeft,
        viewWidth: row.clientWidth,
        contentWidth: row.scrollWidth,
        itemStart: tab.offsetLeft,
        itemWidth: tab.offsetWidth,
      },
      REVEAL_PAD_PX,
    )
    if (left !== null) row.scrollTo({ left })
  }

  function onTabKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const nextIdx = nextTabIndex(e.key, selected, days.length)
    if (nextIdx === null) return
    e.preventDefault()
    select(nextIdx, true)
  }

  return (
    <main className={`${hueScope.scope} ${board.page}`} style={pageToneVars(hue) as unknown as CSSProperties}>
      <header className={board.header}>
        <span className={board.eyebrow}>{cur.year} {SEASON_LABEL[cur.season]}季 · 本周排期</span>
        <h1 className={board.title}>放送表</h1>
        <p className={board.sub}>
          <span>近 7 日共 {totalCount} 集更新 · 时间按本机时区显示</span>
          {freshnessNode}
        </p>
      </header>

      <div ref={rowRef} className={board.tabs} role="tablist" aria-label="本周日期" onKeyDown={onTabKeyDown}>
        {days.map((d, i) => {
          const on = i === selected
          return (
            <button
              key={d.key}
              ref={(el) => { tabRefs.current[i] = el }}
              type="button"
              role="tab"
              id={tabId(i)}
              aria-selected={on}
              aria-controls={panelId(i)}
              tabIndex={on ? 0 : -1}
              className={board.tab}
              onClick={() => select(i, false)}
            >
              <span className={board.tabText}>
                <span className={board.tabLabel}>
                  {d.isToday ? <span className={board.todayDot} aria-hidden="true" /> : null}
                  {d.label}
                </span>
                <span className={board.tabDate}>{d.md}</span>
              </span>
              <span className={board.tabCount}>{d.items.length}<span className="visually-hidden"> 集</span></span>
            </button>
          )
        })}
      </div>

      <div className={board.content}>
        <div className={board.panels}>
          {days.map((d, i) => (
            <DayPanelView key={d.key} day={d} index={i} active={i === selected} nowMs={nowMs} progress={progress} />
          ))}
        </div>

        <aside className={board.aside}>
          <section className={`${board.box} ${board.chart}`} aria-labelledby="schedule-week-chart">
            <div className={board.boxHead}>
              <h2 id="schedule-week-chart" className={board.boxTitle}>本周更新</h2>
              <span className={board.chartHint} aria-hidden="true">点击切换</span>
            </div>
            <div className={board.bars} role="group" aria-labelledby="schedule-week-chart">
              {days.map((d, i) => (
                <button
                  key={d.key}
                  type="button"
                  className={board.bar}
                  aria-pressed={i === selected}
                  aria-controls={panelId(i)}
                  onClick={() => select(i, false)}
                >
                  <span className={board.barCount}>{d.items.length}<span className="visually-hidden"> 集</span></span>
                  <span
                    className={board.barFill}
                    style={{ '--bar-h': `${barHeight(d.items.length, maxDayCount)}px` } as CSSProperties}
                  />
                  <span className={board.barDay}>{d.short}</span>
                </button>
              ))}
            </div>
          </section>

          {followEntries ? (
            <section className={`${board.box} ${board.mine}`} aria-labelledby="schedule-mine">
              <h2 id="schedule-mine" className={`${board.boxTitle} ${board.mineHeading}`}>我追的 · 本周</h2>
              {followEntries.length === 0 ? (
                <p className={board.mineEmpty}>你正在追的作品本周暂无新更新。</p>
              ) : (
                <ol className={board.followList}>
                  {followEntries.slice(0, MAX_FOLLOW_ROWS).map((e) => {
                    const it = e.item
                    return (
                      <li key={it.key}>
                        <MediaEntryLink
                          id={it.id}
                          className={board.followRow}
                          style={cardToneVars(it.hue) as unknown as CSSProperties}
                        >
                          <span className={board.followCover}>
                            <CoverImage src={it.cover} className={board.followImg} />
                          </span>
                          <span className={board.followText}>
                            <span className={board.followTitle}>{it.title}</span>
                            <span className={board.followWhen}>
                              {whenLabel(it.at, nowMs)} · 第 {it.ep} 集
                            </span>
                          </span>
                          <span className={board.followState}>{followText(e)}</span>
                        </MediaEntryLink>
                      </li>
                    )
                  })}
                </ol>
              )}
              {followEntries.length > MAX_FOLLOW_ROWS ? (
                <RouteLink to="/lists" className={board.allLink}>
                  全部在追 <Icon name="right" size={14} />
                </RouteLink>
              ) : null}
            </section>
          ) : (
            <section className={`${board.box} ${board.prompt}`} aria-labelledby="schedule-signin">
              <h2 id="schedule-signin" className={board.boxTitle}>我追的 · 本周</h2>
              <p className={board.promptBody}>登录追番账号后，这里会自动标出你正在追的本周更新与未看集数。</p>
              <RouteLink to="/settings#account" className={board.cta}>去设置登录</RouteLink>
            </section>
          )}

          <RouteLink
            to="/seasonal"
            search={{ season: next.season, year: next.year }}
            className={`${board.box} ${board.nextLink}`}
          >
            <span className={board.nextText}>
              <span className={board.nextTitle}>看看下季新番</span>
              <span className={board.nextSub}>{next.year} 年{SEASON_LABEL[next.season]}季目录</span>
            </span>
            <span className={board.chevron} aria-hidden="true">
              <Icon name="right" size={18} />
            </span>
          </RouteLink>
        </aside>
      </div>
    </main>
  )
}
