import { describe, expect, it } from 'vitest'
import type { SourceOutcome } from '../../lib/endpoints'
import { describeOutcome, formatLatency, isSourceEnabled } from './sourceState'

function outcome(overrides: Partial<SourceOutcome> = {}): SourceOutcome {
  return {
    source: 'src-a',
    state: 'ok',
    count: 0,
    rawCount: 0,
    dropped: 0,
    latencyMs: 120,
    ...overrides,
  }
}

describe('describeOutcome', () => {
  it('ok：短文案是条数，描述带「正常」', () => {
    const view = describeOutcome(outcome({ state: 'ok', count: 12, rawCount: 12 }))
    expect(view.label).toBe('12 条')
    expect(view.description).toBe('正常 · 12 条')
  })

  it.each<[SourceOutcome['state'], string]>([
    ['zero', '无结果'],
    ['dead', '源异常'],
    ['failed', '连接失败'],
    ['disabled', '已禁用'],
  ])('%s → 「%s」', (state, expected) => {
    const view = describeOutcome(outcome({ state }))
    expect(view.label).toBe(expected)
    expect(view.description).toBe(expected)
  })

  it('dead 与 zero 的文案必须不同（决议 CQ3：规则失效不能伪装成无结果）', () => {
    expect(describeOutcome(outcome({ state: 'dead' })).label).not.toBe(
      describeOutcome(outcome({ state: 'zero' })).label,
    )
  })

  it('tooltip 按行拼 reason / detail / 字段缺口 / 丢弃数 / 耗时', () => {
    const view = describeOutcome(
      outcome({
        state: 'dead',
        rawCount: 30,
        dropped: 30,
        reason: '规则解析不出任何条目',
        detail: 'selector "item > title" matched 0 nodes',
        fieldGaps: ['size', 'date'],
        latencyMs: 1543,
      }),
    )
    expect(view.tooltip?.split('\n')).toEqual([
      '规则解析不出任何条目',
      'selector "item > title" matched 0 nodes',
      '全空字段：size、date',
      '上游 30 条，丢弃 30 条',
      '耗时 1.5 s',
    ])
  })

  it('没有任何可说的信息时 tooltip 为 undefined（不挂空 title）', () => {
    expect(describeOutcome(outcome({ state: 'ok', count: 1, latencyMs: -1 })).tooltip).toBeUndefined()
  })

  it('disabled 的 tooltip 固定说明本次未请求', () => {
    expect(describeOutcome(outcome({ state: 'disabled', reason: '被忽略' })).tooltip).toBe(
      '已禁用，本次未请求该源',
    )
  })
})

describe('isSourceEnabled', () => {
  it('只有 disabled 算未启用；坏掉的源仍是启用状态', () => {
    expect(isSourceEnabled(outcome({ state: 'disabled' }))).toBe(false)
    expect(isSourceEnabled(outcome({ state: 'dead' }))).toBe(true)
    expect(isSourceEnabled(outcome({ state: 'ok' }))).toBe(true)
  })
})

describe('formatLatency', () => {
  it.each<[number, string]>([
    [0, '0 ms'],
    [120, '120 ms'],
    [999.6, '1000 ms'],
    [1000, '1 s'],
    [1543, '1.5 s'],
    [-1, ''],
    [NaN, ''],
  ])('%s → %s', (input, expected) => {
    expect(formatLatency(input)).toBe(expected)
  })
})
