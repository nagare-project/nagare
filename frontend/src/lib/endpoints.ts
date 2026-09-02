import { apiFetch } from './api'

/**
 * 后端契约层。
 * M1：/api/library · /api/play · /api/player/* · /api/settings · /api/animego/*
 * M2：/api/search · /api/sources/*（声明式规则引擎）
 * M4：/api/mpv/detect · /api/update* · /api/shutdown（打包后的运行时兜底）
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

/** 后端运行的操作系统（决定 mpv 安装引导与退出提示的措辞） */
export type Platform = 'darwin' | 'linux' | 'windows'

/** mpv 是怎么被找到的：显式配置 / 随包内置 / PATH / 各平台常见安装位置 */
export type MpvSource = 'explicit' | 'bundled' | 'path' | 'known'

/** found=false 时的安装引导（按平台生成）；windows 的 command 为空串（只给 url） */
export interface MpvInstallGuide {
  command: string
  url: string
  note: string
}

export interface MpvInfo {
  found: boolean
  path?: string
  version?: string
  source?: MpvSource
  /** found=false 时的一句话提示 */
  hint?: string
  /** 仅 found=false 时出现 */
  install?: MpvInstallGuide
}

export interface AnimegoInfo {
  loggedIn: boolean
  email?: string
  baseUrl: string
}

/** GET /api/settings 的 data 载荷 */
export interface SettingsData {
  version: string
  platform: Platform
  arch: string
  /** 配置 / state.json 所在目录 */
  dataDir: string
  /** 日志文件路径（「复制诊断信息」的落点） */
  logPath: string
  mpv: MpvInfo
  animego: AnimegoInfo
}

// ---------- 更新（M4：只提示，不自更新） ----------

/** GET /api/update · POST /api/update/check · POST /api/update/config 共用的 data 载荷 */
export interface UpdateView {
  /** 是否开启自动检查（每天最多向 GitHub 查一次） */
  enabled: boolean
  current: string
  /** 尚未检查过时为空串 */
  latest: string
  available: boolean
  /** 新版本的下载页；未检查过时为空串 */
  url: string
  /** 上次检查的时间戳；从未检查过为 null */
  checkedAt: number | null
  /** 上次检查失败的中文原因；成功为空串 */
  error: string
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

/** 重新探测 mpv（用户装完后不用重启）；返回与 settings.mpv 同构的结果 */
export function redetectMpv(): Promise<MpvInfo> {
  return requestJson<MpvInfo>('/api/mpv/detect', 'POST')
}

export function fetchUpdate(): Promise<UpdateView> {
  return apiFetch<UpdateView>('/api/update')
}

/** 立即向 GitHub Releases 查一次；查询失败不抛，落在返回值的 error 字段里 */
export function checkUpdate(): Promise<UpdateView> {
  return requestJson<UpdateView>('/api/update/check', 'POST')
}

export function setUpdateEnabled(enabled: boolean): Promise<UpdateView> {
  return requestJson<UpdateView>('/api/update/config', 'POST', { enabled })
}

/** 让后端进程退出；响应后约 100ms 进程结束，之后页面的任何请求都会失败 */
export function shutdownNagare(): Promise<void> {
  return requestJson<Record<string, never>>('/api/shutdown', 'POST').then(() => undefined)
}

// ---------- 磁力搜索 · 源管理（M2） ----------

/**
 * 一个源在一次搜索 / 自检里的结局（决议 CQ3：零结果 ≠ 规则失效）。
 * - ok：上游正常，解出 count 条
 * - zero：上游正常，确实没有结果
 * - dead：上游有条目但规则一条都解不出 —— 界面必须显示「源异常」而不是「无结果」
 * - failed：网络 / HTTP / 解码失败
 * - disabled：用户已禁用，本次没有请求
 */
export type SourceState = 'ok' | 'zero' | 'dead' | 'failed' | 'disabled'

export interface SourceOutcome {
  /** 规则 id */
  source: string
  state: SourceState
  /** 解出的条数 */
  count: number
  /** 上游原始条数（解析前） */
  rawCount: number
  /** 被丢弃的条数（缺关键字段等） */
  dropped: number
  /** 全空的可选字段名（规则可能漏了这些字段的选择器） */
  fieldGaps?: string[]
  /** 中文原因（给用户看） */
  reason?: string
  /** 技术细节（挂 tooltip） */
  detail?: string
  latencyMs: number
}

/** 一条磁力搜索结果 */
export interface SearchItem {
  title: string
  magnet: string
  /** 人类可读的体积串，后端原样透传；可为空串 */
  size: string
  fansub: string | null
  /** 原样字符串，界面不猜格式 */
  date: string | null
  /** 来源规则 id */
  source: string
  provider?: string
  seeders?: number
  infohash?: string
}

/** GET /api/search?q= 的 data 载荷 */
export interface SearchResult {
  query: string
  items: SearchItem[]
  sources: SourceOutcome[]
}

/** 已加载的一条规则（源） */
export interface SourceInfo {
  id: string
  name: string
  homepage: string
  enabled: boolean
  capabilities: { seeders: boolean; priority: number }
  /** 规则是否自带探活关键词；false 时「自检」不可用 */
  hasSelfTest: boolean
}

/** 规则来源配置与加载状态 */
export interface RulesInfo {
  /** 规则仓库的 HTTPS 地址；空串表示未配置 */
  remoteUrl: string
  /** 本机规则目录（开发用）；空串表示未配置 */
  localDir: string
  /** 实际生效的规则目录 */
  dir: string
  loaded: number
  /** 加载失败的规则文件及原因，非空必须让人看见 */
  errors: string[]
  lastLoadedAt: number | null
  lastSyncAt: number | null
}

/** GET /api/sources 的 data 载荷 */
export interface SourcesData {
  sources: SourceInfo[]
  rules: RulesInfo
}

/** POST /api/sources/reload 的 data 载荷 */
export interface ReloadResult {
  loaded: number
  errors: string[]
}

/** POST /api/sources/sync 的 data 载荷 */
export interface SyncResult {
  added: number
  updated: number
  removed: number
  errors: string[]
}

/** POST /api/sources/config 的 body：字段均可选，缺席表示不改（当前表单总是两项都发） */
export interface RulesConfigPatch {
  remoteUrl?: string
  localDir?: string
}

/** 关键词搜索；q 走 encodeURIComponent（中文 / `&` / `#` 都不能裸露在 query 里） */
export function searchMagnets(query: string): Promise<SearchResult> {
  return apiFetch<SearchResult>(`/api/search?q=${encodeURIComponent(query)}`)
}

export function fetchSources(): Promise<SourcesData> {
  return apiFetch<SourcesData>('/api/sources')
}

export function setSourceEnabled(id: string, enabled: boolean): Promise<void> {
  return requestJson<Record<string, never>>(
    `/api/sources/${encodeURIComponent(id)}/enabled`,
    'POST',
    { enabled },
  ).then(() => undefined)
}

/** 用规则自带的关键词探活，返回与搜索同构的 SourceOutcome */
export function selfCheckSource(id: string): Promise<SourceOutcome> {
  return requestJson<SourceOutcome>(`/api/sources/${encodeURIComponent(id)}/selfcheck`, 'POST')
}

export function reloadSources(): Promise<ReloadResult> {
  return requestJson<ReloadResult>('/api/sources/reload', 'POST')
}

export function updateRulesConfig(patch: RulesConfigPatch): Promise<RulesInfo> {
  return requestJson<RulesInfo>('/api/sources/config', 'POST', patch)
}

/** 从 remoteUrl 拉取规则；remoteUrl 为空时后端 400（中文报错原样透出） */
export function syncSources(): Promise<SyncResult> {
  return requestJson<SyncResult>('/api/sources/sync', 'POST')
}
