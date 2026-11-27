import { describe, expect, it } from 'vitest'
import {
  BAR_MAX_PX,
  BAR_MIN_PX,
  barHeight,
  buildWeek,
  dayTimeline,
  followingThisWeek,
  nextTabIndex,
  revealScrollLeft,
  rowFollowState,
} from './board'
import type { ScheduleAiring } from '../endpoints'

const TZ = 'Asia/Shanghai'
// 2026-09-24 12:00:00+08:00 (Thursday)
const NOW = Date.parse('2026-09-24T12:00:00+08:00')

function airing(over: Partial<ScheduleAiring>): ScheduleAiring {
  return {
    anilistId: 100,
    episode: 5,
    airingAt: Math.floor(Date.parse('2026-09-24T20:00:00+08:00') / 1000),
    title: '测试新番',
    inLibrary: false,
    ...over,
  }
}

describe('lib/schedule/board', () => {
  it('按本机时区分桶成连续 7 天并挑选每日评分最高的有色作品作为主色', () => {
    const days = buildWeek(
      [
        airing({ anilistId: 1, title: '低分红', hue: 25, score: 72, airingAt: Math.floor(Date.parse('2026-09-24T10:00:00+08:00') / 1000) }),
        airing({ anilistId: 2, title: '高分蓝', hue: 245, score: 88, airingAt: Math.floor(Date.parse('2026-09-24T21:00:00+08:00') / 1000) }),
        airing({ anilistId: 3, title: '明日新番', hue: 140, score: 80, airingAt: Math.floor(Date.parse('2026-09-25T23:00:00+08:00') / 1000) }),
      ],
      NOW,
      TZ,
    )
    expect(days).toHaveLength(7)
    expect(days[0].label).toBe('今天')
    expect(days[0].md).toBe('9/24')
    expect(days[0].hue).toBe(245)
    expect(days[0].items.map((i) => i.id)).toEqual([1, 2])
    expect(days[1].label).toBe('明天')
    expect(days[1].hue).toBe(140)
    expect(days[2].items).toEqual([])
    expect(days[2].hue).toBeNull()
  })

  it('按分钟分组并把「现在」标尺放在首个未完全播出的时间段之前', () => {
    const t = dayTimeline(
      [
        { id: 1, at: Date.parse('2026-09-24T10:00:00+08:00') },
        { id: 2, at: Date.parse('2026-09-24T12:30:00+08:00') },
        { id: 3, at: Date.parse('2026-09-24T12:30:20+08:00') },
        { id: 4, at: Date.parse('2026-09-24T22:00:00+08:00') },
      ],
      NOW,
    )
    expect(t.groups).toHaveLength(3)
    expect(t.groups[0].aired).toBe(true)
    expect(t.groups[1].slots).toHaveLength(2)
    expect(t.nowIndex).toBe(1)
  })

  it('计算本周追番状态：已播未看 / 已看 / 落后 N 集 / 待更新', () => {
    const days = [
      {
        items: [
          { id: 1, ep: 5, at: NOW - 60_000 },
          { id: 2, ep: 5, at: NOW - 60_000 },
          { id: 3, ep: 6, at: NOW + 3600_000 },
          { id: 4, ep: 6, at: NOW + 7200_000 },
        ],
      },
    ]
    const entries = followingThisWeek(days, { 1: 4, 2: 5, 3: 3, 4: 5 }, NOW)
    expect(entries.map((e) => [e.item.id, e.state, e.behind])).toEqual([
      [1, 'unwatched', 0],
      [2, 'watched', 0],
      [3, 'behind', 2],
      [4, 'upcoming', 0],
    ])
    expect(rowFollowState(5, 4, true)).toBe('unwatched')
    expect(rowFollowState(5, 5, true)).toBe('following')
    expect(rowFollowState(5, undefined, true)).toBeNull()
  })

  it('周柱状图高度、横排滚动与方向键计算', () => {
    expect(barHeight(0, 10)).toBe(BAR_MIN_PX)
    expect(barHeight(10, 10)).toBe(BAR_MAX_PX)
    expect(barHeight(5, 10)).toBe(48)
    expect(nextTabIndex('ArrowRight', 6, 7)).toBe(0)
    expect(nextTabIndex('ArrowLeft', 0, 7)).toBe(6)
    expect(nextTabIndex('Home', 4, 7)).toBe(0)
    expect(nextTabIndex('End', 2, 7)).toBe(6)
    expect(nextTabIndex('Tab', 2, 7)).toBeNull()
    expect(revealScrollLeft({ viewLeft: 0, viewWidth: 300, contentWidth: 600, itemStart: 280, itemWidth: 80 }, 20)).toBe(80)
    expect(revealScrollLeft({ viewLeft: 0, viewWidth: 300, contentWidth: 600, itemStart: 40, itemWidth: 80 }, 20)).toBeNull()
  })
})
