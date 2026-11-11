// 发现页上的时刻与日期文字。
//
// Intl 只用来取数字部分（en-US），字由下面的表给：各家引擎对本地化日期串的写法不一致
// （「周日19:00」与「周日 19:00」），这样同一个输入在哪里都得到同一串字。
// timeZone 为 undefined 就是运行环境自己的时区 —— nagare 跑在用户自己的机器上，「今天」按本机算。
//
// 纯函数：不碰 DOM、不碰 React。

export interface ClockParts {
  /** 这个时区里的日期，`YYYY-MM-DD` */
  dayKey: string
  /** 0 = 周日 */
  weekday: number
  /** 00–23 */
  hh: string
  /** 00–59 */
  mm: string
}

const WEEKDAY_INDEX: Record<string, number> = { Sun: 0, Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6 }

const formatters = new Map<string, Intl.DateTimeFormat>()

function partsFormatter(timeZone: string | undefined): Intl.DateTimeFormat {
  const key = timeZone ?? ''
  let fmt = formatters.get(key)
  if (!fmt) {
    fmt = new Intl.DateTimeFormat('en-US', {
      timeZone,
      hourCycle: 'h23',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      weekday: 'short',
      hour: '2-digit',
      minute: '2-digit',
    })
    formatters.set(key, fmt)
  }
  return fmt
}

export function clockParts(ms: number, timeZone?: string): ClockParts {
  const parts: Record<string, string> = {}
  for (const p of partsFormatter(timeZone).formatToParts(new Date(ms))) parts[p.type] = p.value
  // 有的引擎在 h23 下仍把午夜写成 24
  const hh = parts.hour === '24' ? '00' : parts.hour
  return {
    dayKey: `${parts.year}-${parts.month}-${parts.day}`,
    weekday: WEEKDAY_INDEX[parts.weekday] ?? 0,
    hh,
    mm: parts.minute,
  }
}

export function hhmm(ms: number, timeZone?: string): string {
  const { hh, mm } = clockParts(ms, timeZone)
  return `${hh}:${mm}`
}

const WEEKDAY_LABELS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'] as const

/** 「周四」；不在一周之内的序号给空串 */
export function weekdayLabel(weekday: number): string {
  return WEEKDAY_LABELS[weekday] ?? ''
}

/** 「周日 19:00」：一部作品下一集什么时候播 */
export function weekdayTime(ms: number, timeZone?: string): string {
  const { weekday, hh, mm } = clockParts(ms, timeZone)
  return `${weekdayLabel(weekday)} ${hh}:${mm}`
}

export interface DayKeyParts {
  year: number
  /** 1–12 */
  month: number
  day: number
  /** 0 = 周日 */
  weekday: number
}

/**
 * `YYYY-MM-DD` 拆成数字；不是真实日期时为 null。
 * 它本来就是日历日期，与时区无关：星期按日期本身算，在哪里都一样。
 */
export function parseDayKey(dayKey: string): DayKeyParts | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(dayKey)
  if (!m) return null
  const [year, month, day] = [Number(m[1]), Number(m[2]), Number(m[3])]
  const date = new Date(Date.UTC(year, month - 1, day))
  if (date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) return null
  return { year, month, day, weekday: date.getUTCDay() }
}

/** dayKey 的下一天；不是真实日期时为 null */
export function nextDayKey(dayKey: string): string | null {
  const parts = parseDayKey(dayKey)
  if (!parts) return null
  return new Date(Date.UTC(parts.year, parts.month - 1, parts.day + 1)).toISOString().slice(0, 10)
}

/** 「周四 9月24日」；不是真实日期时给空串 */
export function dayHeader(dayKey: string): string {
  const parts = parseDayKey(dayKey)
  if (!parts) return ''
  return `${weekdayLabel(parts.weekday)} ${parts.month}月${parts.day}日`
}
