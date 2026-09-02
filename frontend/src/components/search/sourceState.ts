import type { SourceOutcome, SourceState } from '../../lib/endpoints'

/**
 * SourceOutcome → 界面文案的纯映射（决议 CQ3 的落点）。
 * 五态各有一个固定短语；「源异常」与「无结果」是两种颜色、两种词，
 * 人为改坏一条规则时用户看到的必须是前者。
 */

/** 各状态的固定短文案 */
export const SOURCE_STATE_LABEL: Record<SourceState, string> = {
  ok: '正常',
  zero: '无结果',
  dead: '源异常',
  failed: '连接失败',
  disabled: '已禁用',
}

export interface SourceStateView {
  /** 徽标 / chip 上的短文案；ok 态显示条数 */
  label: string
  /** 读屏用的完整状态描述 */
  description: string
  /** hover / focus 提示：reason · detail · 字段缺口 · 丢弃数 · 耗时；没有可说的就 undefined */
  tooltip: string | undefined
}

/** 该源本次是否处于启用状态（disabled 之外都算启用，哪怕它坏了） */
export function isSourceEnabled(outcome: SourceOutcome): boolean {
  return outcome.state !== 'disabled'
}

/** 毫秒耗时 → `123 ms` / `1.2 s` */
export function formatLatency(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return ''
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${parseFloat((ms / 1000).toFixed(1))} s`
}

export function describeOutcome(outcome: SourceOutcome): SourceStateView {
  const stateLabel = SOURCE_STATE_LABEL[outcome.state]
  const label = outcome.state === 'ok' ? `${outcome.count} 条` : stateLabel
  const description = outcome.state === 'ok' ? `${stateLabel} · ${outcome.count} 条` : stateLabel
  return { label, description, tooltip: buildTooltip(outcome) }
}

/** 逐行拼 tooltip；disabled 只说「本次未请求」，其余按有什么说什么 */
function buildTooltip(outcome: SourceOutcome): string | undefined {
  if (outcome.state === 'disabled') return '已禁用，本次未请求该源'

  const lines: string[] = []
  if (outcome.reason !== undefined && outcome.reason !== '') lines.push(outcome.reason)
  if (outcome.detail !== undefined && outcome.detail !== '') lines.push(outcome.detail)
  if (outcome.fieldGaps !== undefined && outcome.fieldGaps.length > 0) {
    lines.push(`全空字段：${outcome.fieldGaps.join('、')}`)
  }
  if (outcome.dropped > 0) {
    lines.push(`上游 ${outcome.rawCount} 条，丢弃 ${outcome.dropped} 条`)
  }
  const latency = formatLatency(outcome.latencyMs)
  if (latency !== '') lines.push(`耗时 ${latency}`)

  return lines.length > 0 ? lines.join('\n') : undefined
}
