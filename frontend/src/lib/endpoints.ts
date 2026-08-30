import { apiFetch } from './api'

/**
 * M1 后端契约层：/api/library · /api/play · /api/player/* · /api/settings · /api/animego/*
 * 类型与 Go 侧信封 data 载荷一一对应；传输细节（token 头、信封解析、错误分类）
 * 全部由 lib/api.ts 的 apiFetch 承担，这里只做「路径 + 形状」。
 */

// ---------- 媒体库 ----------

/** 用户添加的库文件夹；error 非空表示上次扫描该文件夹时出错（路径失效等） */
export interface LibraryFolder {
  id: string
  path: string
  addedAt: number
  error?: string
}

/** 单个视频文件的观看进度 */
export interface ItemProgress {
  positionSec: number
  durationSec: number
  completed: boolean
}

/** 库中的一个视频文件 */
export interface LibraryItem {
  fileId: string
  fileName: string
  /** 解析出的集号；解析不出（剧场版/花絮等）为 null，界面回退显示文件名 */
  episode: number | null
  /** main / sp / ova / ncop / nced / menu / bonus / movie / … */
  kind: string
  resolution: string | null
  sizeBytes: number
  progress: ItemProgress | null
}

/** 簇内分组（正片 / SP / NCOP…），items 已按 sortMode 排好序 */
export interface LibraryGroup {
  groupKey: string
  label: string
  sortMode: 'episode' | 'alpha'
  items: LibraryItem[]
}

/** 一个「作品簇」：同一部番的所有文件归到一起 */
export interface LibraryCluster {
  clusterKey: string
  title: string
  season: number | null
  /** 归簇置信度 0–1；低于阈值时界面标「低置信」 */
  confidence: number
  episodeCount: number
  groups: LibraryGroup[]
}

/** GET /api/library 的 data 载荷 */
export interface LibraryData {
  folders: LibraryFolder[]
  clusters: LibraryCluster[]
  scannedAt: number | null
}

/** 一次扫描的统计结果 */
export interface ScanStats {
  videos: number
  clusters: number
}

/** POST /api/library/folders 的 data 载荷 */
export interface AddFolderData {
  folder: { id: string; path: string; addedAt: number }
  stats: ScanStats
}

// ---------- 播放 ----------

export type DanmakuState = 'loading' | 'ok' | 'unmatched' | 'unavailable' | 'degraded' | 'none'

/** 弹幕装载状态：ok 时带条数，异常时带原因（reason 供 tooltip 展示） */
export interface DanmakuStatus {
  state: DanmakuState
  count?: number
  reason?: string
}

/** POST /api/play 的 data 载荷 */
export interface PlayData {
  title: string
  danmaku: DanmakuStatus
}

/** 正在播放时的完整状态 */
export interface PlayingStatus {
  playing: true
  fileId: string
  title: string
  position: number
  duration: number
  paused: boolean
  danmaku: DanmakuStatus
}

/** GET /api/player/status 的 data 载荷（判别联合：以 playing 收窄） */
export type PlayerStatus = PlayingStatus | { playing: false }

// ---------- 设置 ----------

export interface MpvInfo {
  found: boolean
  version?: string
  path?: string
  /** found=false 时的安装引导文案（按平台生成） */
  hint?: string
}

export interface AnimegoInfo {
  loggedIn: boolean
  email?: string
  baseUrl: string
}

/** GET /api/settings 的 data 载荷 */
export interface SettingsData {
  version: string
  mpv: MpvInfo
  animego: AnimegoInfo
}

// ---------- 请求函数 ----------

/** 带 JSON body 的变更请求（apiFetch 会在此基础上补 token 头） */
function requestJson<T>(path: string, method: 'POST' | 'DELETE', body?: unknown): Promise<T> {
  return apiFetch<T>(path, {
    method,
    ...(body === undefined
      ? {}
      : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  })
}

export function fetchLibrary(): Promise<LibraryData> {
  return apiFetch<LibraryData>('/api/library')
}

export function addLibraryFolder(path: string): Promise<AddFolderData> {
  return requestJson<AddFolderData>('/api/library/folders', 'POST', { path })
}

export function removeLibraryFolder(id: string): Promise<void> {
  return requestJson<Record<string, never>>(
    `/api/library/folders/${encodeURIComponent(id)}`,
    'DELETE',
  ).then(() => undefined)
}

export function rescanLibrary(): Promise<{ stats: ScanStats }> {
  return requestJson<{ stats: ScanStats }>('/api/library/rescan', 'POST')
}

export function playFile(fileId: string): Promise<PlayData> {
  return requestJson<PlayData>('/api/play', 'POST', { fileId })
}

export function fetchPlayerStatus(): Promise<PlayerStatus> {
  return apiFetch<PlayerStatus>('/api/player/status')
}

export function stopPlayer(): Promise<void> {
  return requestJson<Record<string, never>>('/api/player/stop', 'POST').then(() => undefined)
}

export function pausePlayer(paused: boolean): Promise<void> {
  return requestJson<Record<string, never>>('/api/player/pause', 'POST', { paused }).then(
    () => undefined,
  )
}

export function fetchSettings(): Promise<SettingsData> {
  return apiFetch<SettingsData>('/api/settings')
}

export function animegoLogin(email: string, password: string): Promise<{ user: { email: string } }> {
  return requestJson<{ user: { email: string } }>('/api/animego/login', 'POST', {
    email,
    password,
  })
}

export function animegoLogout(): Promise<void> {
  return requestJson<Record<string, never>>('/api/animego/logout', 'POST').then(() => undefined)
}
