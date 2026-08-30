/**
 * 展示层纯函数：时长、字节数、集号、时间戳的格式化。
 * 全部无副作用、不碰浏览器全局，可在 vitest 的 node 环境直接表测。
 */

/** 时间戳单位启发：小于该阈值按「秒」处理，否则按「毫秒」。
 *  Go 侧 time.Unix() 给秒、JS Date.now() 给毫秒，契约未写死单位，这里兼容两者。
 *  1e12 毫秒 ≈ 2001 年，1e12 秒 ≈ 33658 年，中间不存在歧义区。 */
const EPOCH_MS_THRESHOLD = 1e12

/** 两位补零 */
function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

/**
 * 秒数 → 播放器时钟样式。
 * 小于一小时：`mm:ss`（分钟补零到两位）；一小时以上：`h:mm:ss`（小时不补零）。
 * 负数 / NaN / Infinity 一律按 0 处理（后端异常值不该把界面炸出 `NaN:NaN`）。
 */
export function formatDuration(totalSeconds: number): string {
  const safe =
    Number.isFinite(totalSeconds) && totalSeconds > 0 ? Math.floor(totalSeconds) : 0
  const hours = Math.floor(safe / 3600)
  const minutes = Math.floor((safe % 3600) / 60)
  const seconds = safe % 60
  if (hours > 0) return `${hours}:${pad2(minutes)}:${pad2(seconds)}`
  return `${pad2(minutes)}:${pad2(seconds)}`
}

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const

/**
 * 字节数 → 人类可读大小（1024 进制，媒体库/下载器惯例）。
 * 精度随数值缩放：>=100 取整、>=10 一位小数、其余两位小数，并去掉末尾零
 * （`1.50 KB` → `1.5 KB`）。非法输入按 `0 B` 处理。
 */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '0 B'
  if (bytes < 1024) return `${Math.floor(bytes)} B`

  let value = bytes
  let unitIndex = 0
  while (value >= 1024 && unitIndex < BYTE_UNITS.length - 1) {
    value /= 1024
    unitIndex += 1
  }
  const decimals = value >= 100 ? 0 : value >= 10 ? 1 : 2
  // parseFloat 去掉 toFixed 产生的末尾零
  return `${parseFloat(value.toFixed(decimals))} ${BYTE_UNITS[unitIndex]}`
}

/**
 * 观看进度百分比（0–100 的整数，供 aria-valuenow 与 width 共用）。
 * duration 非正或非有限值时返回 0 —— 除零不该变成 NaN% 的进度条。
 */
export function progressPercent(positionSec: number, durationSec: number): number {
  if (!Number.isFinite(positionSec) || !Number.isFinite(durationSec)) return 0
  if (durationSec <= 0) return 0
  const ratio = positionSec / durationSec
  return Math.round(Math.min(1, Math.max(0, ratio)) * 100)
}

/**
 * 集号展示：整数补零到两位（`3` → `03`，`100` → `100`），
 * 半集这类小数原样保留（`3.5` → `3.5`）。
 */
export function formatEpisode(episode: number): string {
  if (Number.isInteger(episode) && episode >= 0) {
    return String(episode).padStart(2, '0')
  }
  return String(episode)
}

/** 秒或毫秒时间戳 → Date（单位启发式见 EPOCH_MS_THRESHOLD） */
function toDate(epoch: number): Date {
  return new Date(epoch < EPOCH_MS_THRESHOLD ? epoch * 1000 : epoch)
}

/** 时间戳 → 本地 `HH:MM`（scannedAt 这类「今天几点扫的」场景） */
export function formatClock(epoch: number): string {
  const date = toDate(epoch)
  return `${pad2(date.getHours())}:${pad2(date.getMinutes())}`
}

/** 时间戳 → 本地 `YYYY-MM-DD`（addedAt 这类「哪天加的」场景） */
export function formatDate(epoch: number): string {
  const date = toDate(epoch)
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`
}

/**
 * 把 unknown 异常收敛成用户可读文案。
 * ApiError / ApiAuthError 的 message 已是后端信封里的中文提示，直接透出；
 * 其余未知形状回退到调用方给的兜底文案（调用方应自行 console.error 留全量上下文）。
 */
export function errorText(err: unknown, fallback: string): string {
  if (err instanceof Error && err.message !== '') return err.message
  return fallback
}
