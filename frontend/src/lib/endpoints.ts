import { ApiError, apiFetch, apiStream } from './api'

/**
 * 后端契约层。
 * M1：/api/library · /api/play · /api/player/* · /api/settings · /api/animego/*
 * M2：/api/search · /api/sources/*（声明式规则引擎）
 * M3：/api/torrent/*（磁力边下边播）
 * M4：/api/mpv/detect · /api/update* · /api/shutdown（打包后的运行时兜底）
 * 类型与 Go 侧信封 data 载荷一一对应；传输细节（token 头、信封解析、错误分类）
 * 全部由 lib/api.ts 的 apiFetch 承担，这里只做「路径 + 形状」。
 */

// ---------- 媒体库 ----------

/**
 * 一类被跳过的东西。reason 是稳定码（分组/断言用），message 与 recovery
 * 是给用户看的中文；两者都由后端给，前端不自己编文案。
 *
 * 现有的 reason：symlink · too-deep · too-small · unreadable-dir · stat-failed。
 * 后端加了新码这里不会红 —— 界面直接渲染 message/recovery，本来就不认识具体的码。
 */
export interface LibraryDropGroup {
  reason: string
  count: number
  message: string
  recovery: string
  /** 完整路径样本，后端每类最多给几条；count 才是真实数量 */
  samples: string[]
}

/** 一个库目录这次扫描跳过了什么。字段缺席 = 一个都没跳过（常态） */
export interface LibraryDrops {
  total: number
  groups: LibraryDropGroup[]
}

/** 用户添加的库文件夹；error 非空表示上次扫描该文件夹时出错（路径失效等） */
export interface LibraryFolder {
  id: string
  path: string
  addedAt: number
  error?: string
  /**
   * 上次扫描跳过了什么。缺席表示什么都没跳过 —— 那是常态，
   * 界面在这种时候【不出】任何提示。
   */
  dropped?: LibraryDrops
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
  /**
   * 可直接放进 <video src> 的本机地址；缺省表示媒体端点未挂载。
   * ⚠️ 后端不转码，能不能播完全取决于浏览器认不认这个编码 ——
   * HEVC / AV1 的 MKV 在多数浏览器上放不了。正常播放路径仍然是 mpv。
   */
  stream?: string
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
  /**
   * 可直接放进 <img src> 的本机封面地址；缺省表示【还没有图】。
   * 没有图是常态不是错误：封面来自播放前的 animego 匹配，
   * 从没播过的番就是没有。界面必须有无图版式。
   */
  cover?: string
  groups: LibraryGroup[]
}

/** 「继续观看」的一张卡片（看过一点、又没看完的条目，按最近观看倒序） */
export interface ContinueItem {
  fileId: string
  title: string
  episodeTitle?: string
  episode: number | null
  episodeCount: number
  cover?: string
  positionSec: number
  durationSec: number
  updatedAt: number
}

/** GET /api/library 的 data 载荷 */
export interface LibraryData {
  folders: LibraryFolder[]
  clusters: LibraryCluster[]
  continueWatching: ContinueItem[]
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
	/** 后端为这次媒体会话生成的稳定标识，用于匹配异步播放失败。 */
  fileId?: string
  title: string
  danmaku: DanmakuStatus
}

/**
 * 上一次「看完标记回写 animego 账号」失败的留痕。
 *
 * 只在失败时出现。它【不随播放结束消失】：失败发生在 mpv 已经退出之后，
 * 挂在会话上等于永远没人看得见 —— 用户以为这一集记上了，
 * 下次打开网站才发现没有，那时已经不知道是哪一集丢的。
 */
export interface SyncFailure {
  state: 'failed'
  title?: string
  episode?: number
  reason?: string
  /** 用户能做的一步动作 */
  recovery?: string
  at?: number
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
  sync?: SyncFailure
  playbackFailure?: PlaybackFailure
}

/** GET /api/player/status 的 data 载荷（判别联合：以 playing 收窄） */
export type PlayerStatus = PlayingStatus | { playing: false; sync?: SyncFailure; playbackFailure?: PlaybackFailure }

export interface PlaybackFailure {
  fileId: string
  reason: string
  at: number
}

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
  /** 磁力边下边播的当前配置与缓存占用（M3） */
  torrent: TorrentSettings
}

// ---------- 更新（M4 阶段 A：只提示；阶段 B：一键更新） ----------

/**
 * 这个安装是怎么装的 —— 决定能不能一键更新：
 * - `app-bundle`：macOS 的 `.app`（整包替换）
 * - `direct`：Windows 安装包 / 便携版、Linux 裸 tar.gz（替换二进制）
 * - `package`：deb / rpm / brew —— 归包管理器管，nagare 不能自己动
 * - `unknown`：认不出来，保守当作不能自更新
 */
export type SelfUpdateChannel = 'app-bundle' | 'direct' | 'package' | 'unknown'

/** 这个安装能不能自更新（GET /api/update 的 selfUpdate 字段） */
export interface SelfUpdateInfo {
  supported: boolean
  channel: SelfUpdateChannel
  /** supported=false 时的中文原因与用户该做什么；后端可能不给，界面要有兜底 */
  reason?: string
  /** 将被替换的东西（二进制路径或 .app 路径），给用户看清楚要动哪里 */
  target: string
}

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
  /** 这个安装能不能一键更新（M4 阶段 B） */
  selfUpdate: SelfUpdateInfo
}

/** POST /api/update/apply 的 data 载荷 */
export interface ApplyUpdateData {
  /** 已经装好的新版本号；调用方拿它跟 /api/health 的版本比对，判断新进程起来了没 */
  version: string
}

// ---------- 请求函数 ----------

/**
 * 带 JSON body 的变更请求（apiFetch 会在此基础上补 token 头）。
 * signal 只有长耗时请求用得上（磁力起播可能跑一两分钟，用户取消时要真正掐断连接，
 * 否则浏览器每域名 6 条并发很快被挂起的请求占满，连状态轮询都发不出去）。
 */
function requestJson<T>(
  path: string,
  method: 'POST' | 'DELETE',
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  return apiFetch<T>(path, {
    method,
    ...(signal === undefined ? {} : { signal }),
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

/**
 * 一键更新：后端下载归档 → minisign 验签 → 按 sha256 校验 → 解包 → 原子替换。
 *
 * **这是一个阻塞请求**：要下 20–120MB 再校验解包，几十秒到几分钟都正常。
 * 响应返回后后端会在约一秒内**重启自己**，期间所有请求都会失败 ——
 * 由调用方轮询 /api/health 等它带着新版本号回来。
 *
 * 刻意不接 signal：替换到一半掐断连接并不会让后端回滚，只会让用户以为取消了。
 */
export function applyUpdate(): Promise<ApplyUpdateData> {
  return requestJson<ApplyUpdateData>('/api/update/apply', 'POST')
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
  /** 标题里解析出的集号；解析不出则缺席 */
  episode?: number
  /** 分组用的字幕组名：规则给的 fansub 优先，没有才用标题里解析的发布组 */
  group?: string
  resolution?: string
  /** main / sp / op / ed …，合集与特典靠它区分 */
  kind?: string
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
/** 磁力选集附带的作品身份：有集号时后端会再向来源插件要 BT 候选（只要 torrent） */
export interface MagnetSearchContext {
  episode: number
  anilistId?: number
  year?: number
  /** 目录里的其他标题（原名/英文名），插件按它们再搜一遍 */
  altTitles?: string[]
}

export function searchMagnets(query: string, context?: MagnetSearchContext): Promise<SearchResult> {
  const params = [`q=${encodeURIComponent(query)}`]
  if (context !== undefined) {
    params.push(`episode=${context.episode}`)
    if (context.anilistId !== undefined) params.push(`anilist=${context.anilistId}`)
    if (context.year !== undefined) params.push(`year=${context.year}`)
    for (const title of context.altTitles ?? []) params.push(`title=${encodeURIComponent(title)}`)
  }
  return apiFetch<SearchResult>(`/api/search?${params.join('&')}`)
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

// ---------- 本地来源插件（Plugin API v1） ----------

export interface SourcePluginConfig {
  enabled: boolean
  executable: string
  root: string
  /** 把路径换回安装包捆绑的插件 */
  useBundled?: boolean
}

/** 安装包捆绑的插件（引擎 + 只含 BT 规则的 repo）；没捆时缺席 */
export interface BundledPluginView {
  executable: string
  root: string
  /** 当前配置用的就是捆绑的这一份 */
  active: boolean
}

export interface SourcePluginManifest {
  id: string
  name: string
  version: string
  protocolVersions: number[]
  sourceSchemaVersions: number[]
  capabilities: string[]
}

export interface SourcePluginStatus {
  phase: 'disabled' | 'starting' | 'ready' | 'stopped' | 'failed'
  url?: string
  manifest?: SourcePluginManifest
  error?: string
  startedAt?: number
}

export interface PluginSource {
  id: string
  name: string
  kind: string
  tier: number
  version: string
  enabled: boolean
  status: 'healthy' | 'degraded' | 'unavailable' | 'interactive_required' | 'disabled'
  capabilities: string[]
}

export interface SourcePluginView {
  config: SourcePluginConfig
  status: SourcePluginStatus
  sources: PluginSource[]
  sourcesError?: string
  bundled?: BundledPluginView
}

export interface SourceResolveRequest {
  schema: 'nagare-resolve-request/v1'
  subject: {
    ids: Record<string, string>
    titles: string[]
    season?: number
    year?: number
  }
  episode: {
    id?: string
    number: string
    absolute?: number
    airDate?: string
    title?: string
  }
  preferences?: {
    subtitleLanguages?: string[]
    maxResolution?: string
    preferredTransports?: string[]
  }
}

export interface SourceCandidate {
  schema: 'nagare-candidate/v1'
  id: string
  sourceId: string
  tier: number
  matchConfidence: number
  match: {
    basis: string[]
    subjectTitle?: string
    episodeNumber?: number
    evidence?: string
  }
  transport: {
    type: 'hls' | 'http' | 'torrent'
    url?: string
    headers?: Record<string, string>
    expiresAt?: number
    magnet?: string
    infoHash?: string
    torrentUrl?: string
    fileIndex?: number
    trackers?: string[]
  }
  metadata: {
    resolution?: string
    subtitleLanguages?: string[]
    channel?: string
    channelTier?: number
    episode?: number
    fansub?: string
    sizeBytes?: number
    seeders?: number
    publishedAt?: string
  }
}

export type SourcePluginEvent =
  | { event: 'candidate'; candidate: SourceCandidate }
  | { event: 'source_error'; sourceId: string; category: string; message: string; retryable: boolean }
  | { event: 'done'; queried: number; succeeded: number; failed: number; durationMs: number }
  | { event: 'host_error'; message: string; afterEvents: number }

export function fetchSourcePlugin(signal?: AbortSignal): Promise<SourcePluginView> {
  return apiFetch<SourcePluginView>('/api/source-plugin', signal === undefined ? undefined : { signal })
}

export function updateSourcePluginConfig(config: SourcePluginConfig): Promise<SourcePluginView> {
  return requestJson<SourcePluginView>('/api/source-plugin/config', 'POST', config)
}

/** 逐行消费候选流；单行上限与后端插件契约同为 1 MiB。 */
export async function streamSourceCandidates(
  request: SourceResolveRequest,
  onEvent: (event: SourcePluginEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const response = await apiStream('/api/source-plugin/candidates', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
    signal,
  })
  if (!response.headers.get('Content-Type')?.toLowerCase().includes('application/x-ndjson')) {
    throw new ApiError('来源插件返回了意外的响应格式', response.status)
  }
  if (response.body === null) {
    throw new ApiError('来源插件没有返回候选流', response.status)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let pending = ''
  let doneSeen = false

  const consume = (line: string): void => {
    if (line.trim() === '') return
    let parsed: unknown
    try {
      parsed = JSON.parse(line)
    } catch (cause) {
      throw new ApiError('来源插件返回了无效的候选事件', response.status, { cause })
    }
    const event = parseSourcePluginEvent(parsed)
    if (event.event === 'host_error') throw new ApiError(event.message, response.status)
    if (doneSeen) throw new ApiError('来源插件在完成后仍返回了事件', response.status)
    onEvent(event)
    if (event.event === 'done') doneSeen = true
  }

  while (true) {
    const { value, done } = await reader.read()
    pending += decoder.decode(value, { stream: !done })
    if (pending.length > 1024 * 1024 && !pending.includes('\n')) {
      await reader.cancel()
      throw new ApiError('来源插件的单条候选事件过大', response.status)
    }
    const lines = pending.split('\n')
    pending = lines.pop() ?? ''
    for (const line of lines) {
      if (line.length > 1024 * 1024) {
        await reader.cancel()
        throw new ApiError('来源插件的单条候选事件过大', response.status)
      }
      consume(line)
    }
    if (done) break
  }
  consume(pending)
  if (!doneSeen) throw new ApiError('来源插件的候选流未正常完成', response.status)
}

// identity 是目录里的作品身份：在线媒体没有文件指纹，弹幕只能靠标题匹配，
// 后端用 anilistId 校验没有命中同名的另一部作品，主标题失手时再拿别名试。
export interface SourcePlaybackIdentity {
  anilistId: number
  altTitles: string[]
}

export function playSourceCandidate(
  candidate: SourceCandidate,
  title: string,
  episode: number,
  identity: SourcePlaybackIdentity,
): Promise<PlayData> {
  return requestJson<PlayData>('/api/source-plugin/play', 'POST', {
    candidate, title, episode, anilistId: identity.anilistId, altTitles: identity.altTitles,
  })
}

function parseSourcePluginEvent(value: unknown): SourcePluginEvent {
  if (!isRecord(value) || typeof value.event !== 'string') {
    throw new ApiError('来源插件返回了未知事件', 200)
  }
  switch (value.event) {
    case 'candidate':
      if (!isCandidate(value.candidate)) break
      return { event: 'candidate', candidate: value.candidate }
    case 'source_error':
      if (typeof value.sourceId !== 'string' || typeof value.category !== 'string' ||
          typeof value.message !== 'string' || typeof value.retryable !== 'boolean') break
      return { event: 'source_error', sourceId: value.sourceId, category: value.category, message: value.message, retryable: value.retryable }
    case 'done':
      if (![value.queried, value.succeeded, value.failed, value.durationMs].every(item => typeof item === 'number')) break
      return { event: 'done', queried: value.queried as number, succeeded: value.succeeded as number, failed: value.failed as number, durationMs: value.durationMs as number }
    case 'host_error':
      if (typeof value.message !== 'string' || typeof value.afterEvents !== 'number') break
      return { event: 'host_error', message: value.message, afterEvents: value.afterEvents }
  }
  throw new ApiError('来源插件返回了格式错误的事件', 200)
}

function isCandidate(value: unknown): value is SourceCandidate {
  if (!isRecord(value) || value.schema !== 'nagare-candidate/v1' || typeof value.id !== 'string' ||
      typeof value.sourceId !== 'string' || typeof value.tier !== 'number' ||
      typeof value.matchConfidence !== 'number' || !isRecord(value.match) ||
      !Array.isArray(value.match.basis) || !value.match.basis.every(item => typeof item === 'string') ||
      !isRecord(value.transport) || !['hls', 'http', 'torrent'].includes(String(value.transport.type)) ||
      !isRecord(value.metadata)) return false
  return true
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/** 从 remoteUrl 拉取规则；remoteUrl 为空时后端 400（中文报错原样透出） */
export function syncSources(): Promise<SyncResult> {
  return requestJson<SyncResult>('/api/sources/sync', 'POST')
}

// ---------- 磁力边下边播（M3） ----------

/** 种子里的一个文件（需要用户选集时由 /api/torrent/play 返回） */
export interface TorrentFile {
  index: number
  name: string
  /** 种子内的相对路径（同名文件靠它区分） */
  path: string
  sizeBytes: number
  /** 解析链解出的集号；解析不出为 null，界面留空 */
  episode: number | null
}

/**
 * 一次磁力播放会话所处的阶段：
 * idle 无会话 · metadata 等元数据（找分享者）· selecting 等用户选集 ·
 * buffering 起播缓冲 · ready 缓冲够了，正在交给 mpv
 */
export type TorrentPhase = 'idle' | 'metadata' | 'selecting' | 'buffering' | 'ready'

/** GET /api/torrent/status 的 data 载荷 */
export interface TorrentStatus {
  /** false 表示后端当前没有活动的磁力会话 */
  active: boolean
  phase: TorrentPhase
  /** 种子名 */
  name?: string
  /** 已选中的文件名 */
  fileName?: string
  infohash?: string
  peers: number
  seeders: number
  /** 字节/秒 */
  downRate: number
  upRate: number
  /** 0–1，起播缓冲进度 */
  buffered: number
  /** 0–1，所选文件已完成比例 */
  progress: number
  cacheBytes: number
  seeding: boolean
  /**
   * 会话已经发生、但不体现在 /api/torrent/play 返回值里的失败中文提示。
   * 目前只有「播放开始之后的分片写盘失败」会走这里 —— 那时没有任何请求在等着
   * 接这个错误，只有轮询看得见，否则用户只会看到进度不动了。
   */
  error?: string
}

/** POST /api/torrent/play 的 body；Plugin API v1 的 BT 候选可以给 magnet 或 .torrent URL。 */
export type TorrentPlayRequest = ({ magnet: string; torrentUrl?: never } | { magnet?: never; torrentUrl: string }) & {
  title?: string
  /**
   * 合集里定位文件用的集号。**当前搜索页不传**：`SearchItem` 没有集号字段，
   * 而后端在缺席时会用同一条解析链从 `title` 里派生，效果一样且少一处会漂移的重复。
   * 留着这个字段是给将来「用户在界面上直接指定第几集」用的。
   */
  episodeHint?: number
  /** 用户在选集弹窗里选定的文件序号（第二次请求才带） */
  fileIndex?: number
}

/**
 * POST /api/torrent/play 的 data 载荷（判别联合：以 needSelection 收窄）。
 * needSelection=true 时后端保留着种子等用户选集 —— 用户放弃时必须 POST /api/torrent/stop 释放。
 */
export type TorrentPlayData =
  | { needSelection: true; files: TorrentFile[] }
  | { needSelection: false; title: string; danmaku: DanmakuStatus }

/** 磁力配置与缓存占用（GET /api/settings 的 torrent 字段） */
export interface TorrentSettings {
  /**
   * 磁力引擎是否可用；false 表示引擎启动失败（磁盘 / 端口问题），
   * 此时所有 /api/torrent/* 返回 503。降级运行可以，但必须让用户看见。
   */
  enabled: boolean
  /** 停止播放后是否继续上传；播放期间的分片交换是 BT 协议必需的，不受此开关影响 */
  seeding: boolean
  /** 用户自己追加的 tracker（内置组之外）；只补给公开种子 */
  trackers: string[]
  /** 是否启用内置的公共 tracker 组（默认开；关掉后只剩用户自填的） */
  useDefaultTrackers: boolean
  /** 内置公共 tracker 组的内容，只读展示 */
  defaultTrackers: string[]
  /** UPnP / NAT-PMP 自动端口映射 */
  portForwarding: boolean
  listenPort: number
  cacheDir: string
  cacheBytes: number
}

/** POST /api/torrent/config 的 data 载荷：配置全量 + 是否需要重启才生效 */
export interface TorrentConfigData extends TorrentSettings {
  /** 端口 / 映射类改动要重启 nagare 才生效 */
  restartRequired: boolean
}

/** POST /api/torrent/config 的 body：字段均可选，缺席表示不改 */
export interface TorrentConfigPatch {
  seeding?: boolean
  trackers?: string[]
  useDefaultTrackers?: boolean
  portForwarding?: boolean
  listenPort?: number
}

/**
 * 发起磁力播放。**这是一个阻塞请求**：要等元数据 + 起播缓冲，10 秒到 2 分钟都正常，
 * 期间由调用方轮询 /api/torrent/status 显示进展。signal 用于用户取消时掐断连接。
 */
export function startTorrentPlay(
  request: TorrentPlayRequest,
  signal?: AbortSignal,
): Promise<TorrentPlayData> {
  return requestJson<TorrentPlayData>('/api/torrent/play', 'POST', request, signal)
}

export function fetchTorrentStatus(): Promise<TorrentStatus> {
  return apiFetch<TorrentStatus>('/api/torrent/status')
}

/** 停止当前磁力会话并释放种子（取消缓冲 / 停止播放 / 放弃选集都走这里） */
export function stopTorrent(): Promise<void> {
  return requestJson<Record<string, never>>('/api/torrent/stop', 'POST').then(() => undefined)
}

export function clearTorrentCache(): Promise<{ cacheBytes: number }> {
  return requestJson<{ cacheBytes: number }>('/api/torrent/cache/clear', 'POST')
}

export function updateTorrentConfig(patch: TorrentConfigPatch): Promise<TorrentConfigData> {
  return requestJson<TorrentConfigData>('/api/torrent/config', 'POST', patch)
}

// ---------- 目录 · 收藏 · 放送（M6） ----------
/** API 数据通过 lib/media.ts 适配，组件继续持有自己的契约。 */
export interface SummaryMediaData {
  anilistId: number; title: string; titleNative?: string; titleEnglish?: string
  cover?: string; banner?: string; trailerId?: string; year?: number; season?: string
  episodes: number | null; score?: number; genres: string[]; description?: string
  status?: string; format?: string; duration?: number; source?: string; startDate?: string; studios?: string[]
  nextAiring?: { episode: number; at: number }; recentAiring?: { episode: number; at: number }
  relations?: { type: string; media: SummaryMediaData }[]; recommendations?: SummaryMediaData[]
  characters?: { name: string; image: string; role: string; actor?: string; actorImage?: string }[]
  episodeTitles?: { episode: number; title: string }[]
}
export interface DiscoverData { sections: { key: string; title: string; items: SummaryMediaData[]; error?: string }[]; fetchedAt: number }
export type CollectionStatus = 'watching' | 'plan_to_watch' | 'completed' | 'dropped'
export interface CollectionEntry { anilistId: number; status: CollectionStatus; currentEpisode: number; score: number | null; media: SummaryMediaData; lastWatchedAt?: number }
export interface CollectionData { loggedIn: boolean; entries: CollectionEntry[] }
export interface CollectionEdit { status: CollectionStatus; progress: number; score: number | null }
export interface ScheduleData { airings: { anilistId: number; episode: number; airingAt: number; title: string; cover?: string; format?: string; inLibrary: boolean }[]; fetchedAt: number }
export const fetchDiscoverData = (signal?: AbortSignal) => apiFetch<DiscoverData>('/api/discover', { signal })
export type SeasonName = 'WINTER' | 'SPRING' | 'SUMMER' | 'FALL'
export interface SeasonalData { season: SeasonName; year: number; items: SummaryMediaData[]; fetchedAt: number }
/** 某一季的全部作品（animego 一页 200 条），后端十分钟缓存 */
export const fetchSeasonalData = (season: SeasonName, year: number, signal?: AbortSignal) =>
  apiFetch<SeasonalData>(`/api/seasonal?season=${season}&year=${year}`, { signal })
export const fetchMediaData = (id: number, signal?: AbortSignal) => apiFetch<SummaryMediaData>(`/api/anime/${id}`, { signal })
export const fetchCollection = () => apiFetch<CollectionData>('/api/lists')
export const saveCollection = (id: number, data: CollectionEdit) => apiFetch<CollectionEntry>(`/api/lists/${id}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ status: data.status, currentEpisode: data.progress, score: data.score }) })
export const deleteCollection = (id: number) => apiFetch(`/api/lists/${id}`, { method: 'DELETE' })
export const fetchSchedule = (signal?: AbortSignal) => apiFetch<ScheduleData>('/api/schedule', { signal })
