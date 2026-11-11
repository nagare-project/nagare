// 横幅上那一行状态：「连载中 · 周日 19:00 更新第 14 集」「已完结 · 共 12 集」。
//
// 下一集的时间只有放送表近七日里的作品才知道，所以时间那段是可选的，没有它状态照样成立。
// 时间怎么写由调用方注入（本机时区）。
//
// 纯函数：不碰 DOM、不碰 React。

export interface Airing {
  /** 毫秒时间戳 */
  at: number
  ep: number
}

/** 严格晚于 now 的第一集；这一分钟刚播出的算已播 */
export function upcomingAiring(airings: readonly Airing[], nowMs: number): Airing | null {
  let best: Airing | null = null
  for (const a of airings) {
    if (a.at <= nowMs) continue
    if (!best || a.at < best.at) best = a
  }
  return best
}

export interface StatusInput {
  /** 上游的原始状态：RELEASING、FINISHED、NOT_YET_RELEASED…… */
  status: string | null
  /** 调用方已经译好的状态名 */
  statusLabel: string
  episodes: number | null
  airings: readonly Airing[]
}

export interface StatusParts {
  label: string
  /** 下一集的时间（知道、且还没到时） */
  when: string | null
  /** 下一集是第几集，或已完结作品的总集数 */
  detail: string | null
  /** 正在连载：状态点会闪 */
  live: boolean
}

export function heroStatusParts(input: StatusInput, nowMs: number, formatWhen: (ms: number) => string): StatusParts {
  const live = input.status === 'RELEASING'
  if (input.status === 'FINISHED') {
    const total = input.episodes && input.episodes > 0 ? `共 ${input.episodes} 集` : null
    return { label: input.statusLabel, when: null, detail: total, live: false }
  }
  const next = upcomingAiring(input.airings, nowMs)
  if (!next) return { label: input.statusLabel, when: null, detail: null, live }
  return { label: input.statusLabel, when: formatWhen(next.at), detail: `更新第 ${next.ep} 集`, live }
}

/** 拼成一行。short：有时间时省掉集数 —— 窄屏的横幅只放得下「连载中 · 周日 19:00」 */
export function statusText(parts: StatusParts, opts: { short?: boolean } = {}): string {
  if (!parts.label) return ''
  if (parts.when) {
    return opts.short || !parts.detail ? `${parts.label} · ${parts.when}` : `${parts.label} · ${parts.when} ${parts.detail}`
  }
  return parts.detail ? `${parts.label} · ${parts.detail}` : parts.label
}
