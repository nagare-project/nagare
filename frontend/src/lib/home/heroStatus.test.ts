import { describe, expect, test } from 'vitest'
import { heroStatusParts, statusText, upcomingAiring } from './heroStatus'

const NOW = 1_000_000
const when = (ms: number) => `T${ms}`

describe('upcomingAiring', () => {
  test('是严格晚于现在的第一集', () => {
    expect(upcomingAiring([{ at: NOW - 10, ep: 3 }, { at: NOW + 50, ep: 5 }, { at: NOW + 20, ep: 4 }], NOW)).toEqual({ at: NOW + 20, ep: 4 })
  })

  test('正在播出的这一集已经不算下一集', () => {
    expect(upcomingAiring([{ at: NOW, ep: 3 }], NOW)).toBeNull()
  })

  test('一集都不知道时为 null', () => {
    expect(upcomingAiring([], NOW)).toBeNull()
  })
})

describe('heroStatusParts / statusText', () => {
  test('连载中、知道下一集', () => {
    const parts = heroStatusParts({ status: 'RELEASING', statusLabel: '连载中', episodes: 14, airings: [{ at: NOW + 100, ep: 14 }] }, NOW, when)
    expect(parts).toEqual({ label: '连载中', when: `T${NOW + 100}`, detail: '更新第 14 集', live: true })
    expect(statusText(parts)).toBe(`连载中 · T${NOW + 100} 更新第 14 集`)
    expect(statusText(parts, { short: true })).toBe(`连载中 · T${NOW + 100}`)
  })

  test('连载中、下一集不知道：只说状态，点照样闪', () => {
    const parts = heroStatusParts({ status: 'RELEASING', statusLabel: '连载中', episodes: null, airings: [] }, NOW, when)
    expect(parts).toEqual({ label: '连载中', when: null, detail: null, live: true })
    expect(statusText(parts)).toBe('连载中')
  })

  test('已完结，带总集数', () => {
    const parts = heroStatusParts({ status: 'FINISHED', statusLabel: '已完结', episodes: 12, airings: [] }, NOW, when)
    expect(statusText(parts)).toBe('已完结 · 共 12 集')
    expect(parts.live).toBe(false)
  })

  test('已完结、总集数不知道', () => {
    expect(statusText(heroStatusParts({ status: 'FINISHED', statusLabel: '已完结', episodes: 0, airings: [] }, NOW, when))).toBe('已完结')
  })

  test('还没开播但排上了', () => {
    const parts = heroStatusParts({ status: 'NOT_YET_RELEASED', statusLabel: '未开播', episodes: 12, airings: [{ at: NOW + 5, ep: 1 }] }, NOW, when)
    expect(statusText(parts)).toBe(`未开播 · T${NOW + 5} 更新第 1 集`)
    expect(parts.live).toBe(false)
  })

  test('没有状态名时整行为空', () => {
    expect(statusText(heroStatusParts({ status: null, statusLabel: '', episodes: null, airings: [] }, NOW, when))).toBe('')
  })
})
