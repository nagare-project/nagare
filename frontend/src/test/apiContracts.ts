/**
 * 客户端类型（lib/endpoints.ts 等）对应的严格解码 schema，给 lib/apiGoldens.test.ts 用。
 *
 * 每个 schema 都标注了它对应的 TS 类型：两边字段不一致时 `bun run typecheck` 报错（见 test/contract.ts）。
 * 所以改 endpoints.ts 里的类型要同步改这里，反过来也一样 —— 这正是要的效果。
 *
 * int / num 的分工：计数、ID、毫秒 / 秒时间戳、字节数用 int；时长、比例、置信度用 num。
 */
import type { AssociationData, LibraryAssociation, LibraryMatch, SyncedRecord } from '../lib/associations'
import type {
  AddFolderData, BundledPluginView, CatalogFreshness, CatalogSearchData, CollectionData, CollectionDeleteData, CollectionEntry,
  ContinueItem, CoverCacheClearResult, CoverCacheInfo, DanmakuStatus, DirListing, DiscoverData, DownloadTask, DownloadsData, GemsData,
  HomeAiringData, HomeMediaData, ItemProgress, LibraryCluster, RankedMediaData,
  LibraryData, LibraryDropGroup, LibraryDrops, LibraryFolder, LibraryGroup, LibraryIdentify, LibraryItem, MpvInfo,
  PlaybackFailure, PlayData, PlayerConfigResult, PlayerStatus, PlayingStatus, PluginSource, PosterTones, RulesInfo, ScanStats,
  ScheduleData, SeasonalData, SelfUpdateInfo, SettingsData, SourceInfo, SourcePluginConfigView, SourcePluginManifest,
  SourcePluginStatus, SourcePluginView, SourcesData, SummaryMediaData, SyncFailure, TorrentConfigData, TorrentFile,
  TorrentPlayData, TorrentSettings, TorrentStatus, UpdateView,
  animegoLogin, rescanLibrary,
} from '../lib/endpoints'
import type { EpisodeOffset } from '../lib/episodeOffset'
import type { SyncRetryResult } from '../lib/syncRetry'
import type { EpisodeMetadata } from '../components/media/types'
import { arr, bool, exact, int, lazy, lit, nullable, num, obj, str, union, type Schema } from './contract'

// ---------- 海报色 · 目录数据的来历（多个接口共用） ----------

const posterTones = exact<PosterTones>()(obj({ hue: num, tone: str, quiet: str, mid: str, low: str, onTone: str }))
const freshness = exact<CatalogFreshness>()(obj({ fetchedAt: str, stale: bool, offline: bool }))

// ---------- 媒体库 ----------

const dropGroup = exact<LibraryDropGroup>()(obj({ reason: str, count: int, message: str, recovery: str, samples: arr(str) }))
const drops = exact<LibraryDrops>()(obj({ total: int, groups: arr(dropGroup) }))
const folder = exact<LibraryFolder>()(obj({ id: str, path: str, addedAt: int }, { error: str, dropped: drops }))
const itemProgress = exact<ItemProgress>()(obj({ positionSec: num, durationSec: num, completed: bool }))
const libraryItem = exact<LibraryItem>()(obj(
  { fileId: str, fileName: str, episode: nullable(int), kind: str, resolution: nullable(str), sizeBytes: int, progress: nullable(itemProgress) },
  { group: nullable(str), stream: str },
))
const libraryGroup = exact<LibraryGroup>()(obj({ groupKey: str, label: str, sortMode: lit('episode', 'alpha'), items: arr(libraryItem) }))
const syncedRecord = exact<SyncedRecord>()(obj({ anilistId: int, episodes: arr(int) }, { title: str }))
const association = exact<LibraryAssociation>()(obj(
  { mode: lit('manual', 'none'), setAt: int },
  { anilistId: int, title: str, syncedElsewhere: arr(syncedRecord) },
))
const match = exact<LibraryMatch>()(obj({ anilistId: int }, { title: str }))
const cluster = exact<LibraryCluster>()(obj(
  { clusterKey: str, title: str, season: nullable(int), confidence: num, episodeCount: int, groups: arr(libraryGroup) },
  { cover: str, association, matched: match, posterTones: nullable(posterTones) },
))
const continueItem = exact<ContinueItem>()(obj(
  { fileId: str, title: str, episode: nullable(int), episodeCount: int, positionSec: num, durationSec: num, updatedAt: int },
  { episodeTitle: str, cover: str, posterTones: nullable(posterTones) },
))
const identify = exact<LibraryIdentify>()(obj({ running: bool, done: int, total: int }, { error: str }))
export const libraryData = exact<LibraryData>()(obj(
  { folders: arr(folder), clusters: arr(cluster), continueWatching: arr(continueItem), scannedAt: nullable(int) },
  { identify },
))
const scanStats = exact<ScanStats>()(obj({ videos: int, clusters: int }))
export const addFolderData = exact<AddFolderData>()(obj({ folder: obj({ id: str, path: str, addedAt: int }), stats: scanStats }))
export const rescanData = exact<Awaited<ReturnType<typeof rescanLibrary>>>()(obj({ stats: scanStats }))
export const associationData = exact<AssociationData>()(obj({ association: nullable(association) }))
export const dirListing = exact<DirListing>()(obj({ path: str, parent: str, dirs: arr(obj({ name: str, path: str })), truncated: bool }))

// ---------- 播放 ----------

const danmaku = exact<DanmakuStatus>()(obj(
  { state: lit('loading', 'ok', 'unmatched', 'unavailable', 'degraded', 'none') },
  { count: int, reason: str },
))
export const playData = exact<PlayData>()(obj({ title: str, danmaku }, { fileId: str, playbackId: str }))
const syncFailure = exact<SyncFailure>()(obj(
  { state: lit('failed') },
  { fileId: str, title: str, episode: int, reason: str, recovery: str, at: int, pending: int },
))
const playbackFailure = exact<PlaybackFailure>()(obj({ fileId: str, reason: str, at: int }, { playbackId: str }))
const playingStatus = exact<PlayingStatus>()(obj(
  { playing: lit(true), fileId: str, title: str, danmaku },
  { position: num, duration: num, paused: bool, sync: syncFailure, playbackFailure },
))
export const playerStatus = exact<PlayerStatus>()(union(
  playingStatus,
  obj({ playing: lit(false) }, { sync: syncFailure, playbackFailure }),
))
export const syncRetryResult = exact<SyncRetryResult>()(obj({ synced: int, remaining: int }, { reason: str }))
const anime4k = lit('off', 'fast', 'hq')
export const playerConfigResult = exact<PlayerConfigResult>()(obj({ anime4k, applied: bool }, { applyError: str }))

// ---------- 设置 · 账号 · 更新 ----------

const mpvSource = lit('explicit', 'bundled', 'path', 'known', 'embedded')
export const mpvInfo = exact<MpvInfo>()(obj(
  { found: bool },
  {
    embedded: bool, cli: obj({ path: str, version: str, source: mpvSource }), path: str, version: str, source: mpvSource,
    hint: str, install: obj({ command: str, url: str, note: str }),
  },
))
const torrentSettings = exact<TorrentSettings>()(obj({
  enabled: bool, seeding: bool, trackers: arr(str), useDefaultTrackers: bool, defaultTrackers: arr(str),
  portForwarding: bool, listenPort: int, cacheDir: str, cacheBytes: int, downloadDir: str, downloadDirIsDefault: bool,
  restartRequired: bool,
}))
export const torrentConfigData: Schema<TorrentConfigData> = torrentSettings
const coverCacheInfo = exact<CoverCacheInfo>()(obj({ bytes: int, count: int, maxBytes: int }))
export const coverCacheClearResult = exact<CoverCacheClearResult>()(obj({ bytes: int, count: int, maxBytes: int, skipped: int }))
export const settingsData = exact<SettingsData>()(obj(
  {
    version: str, platform: lit('darwin', 'linux', 'windows'), arch: str, dataDir: str, logPath: str, mpv: mpvInfo,
    animego: obj({ loggedIn: bool, baseUrl: str }, { email: str }),
    player: obj({ anime4k }),
    background: obj({ mode: lit('dock', 'menubar', 'tray', 'window', 'none') }),
  },
  { torrent: torrentSettings, desktop: obj({ window: bool }, { fallback: str }), coverCache: nullable(coverCacheInfo) },
))
export const loginData = exact<Awaited<ReturnType<typeof animegoLogin>>>()(obj({ user: obj({ email: str }) }))
const selfUpdate = exact<SelfUpdateInfo>()(obj(
  { supported: bool, channel: lit('app-bundle', 'direct', 'package', 'unknown'), target: str },
  { reason: str },
))
export const updateView = exact<UpdateView>()(obj({
  enabled: bool, current: str, latest: str, available: bool, url: str, checkedAt: nullable(int), error: str, selfUpdate,
}))

// ---------- 目录 · 收藏 ----------

const airing = obj({ episode: int, at: int })
export const summaryMedia: Schema<SummaryMediaData> = exact<SummaryMediaData>()(obj(
  { anilistId: int, title: str, episodes: nullable(int), genres: arr(str) },
  {
    titleNative: str, titleRomaji: str, titleEnglish: str, cover: str, banner: str, trailerId: str, year: int, season: str,
    score: int, description: str, status: str, format: str, duration: int, source: str, startDate: str, studios: arr(str),
    nextAiring: airing, recentAiring: airing,
    relations: arr(obj({ type: str, media: lazy((): Schema<SummaryMediaData> => summaryMedia) })),
    recommendations: arr(lazy((): Schema<SummaryMediaData> => summaryMedia)),
    characters: arr(obj({ name: str, image: str, role: str }, { actor: str, actorImage: str })),
    episodeTitles: arr(obj({ episode: int, title: str })),
    freshness, posterTones: nullable(posterTones),
  },
))
const seasonName = lit('WINTER', 'SPRING', 'SUMMER', 'FALL')
const homeMediaFields = {
  required: { anilistId: int, title: str, hue: nullable(num), episodes: nullable(int), genres: arr(str) },
  optional: {
    titleNative: str, titleRomaji: str, titleEnglish: str, cover: str, banner: str, score: int, bangumiScore: num, season: seasonName, year: int,
    status: str, format: str, description: str,
  },
}
const homeMedia = exact<HomeMediaData>()(obj(homeMediaFields.required, homeMediaFields.optional))
const rankedMedia = exact<RankedMediaData>()(obj({ ...homeMediaFields.required, rank: int, watchers: int }, homeMediaFields.optional))
const homeAiring = exact<HomeAiringData>()(obj(
  { scheduleId: int, anilistId: int, episode: int, airingAt: int, title: str, hue: nullable(num) },
  { cover: str },
))
function homeSection<T>(item: Schema<T>) {
  return obj({ items: arr(item) }, { error: str })
}
export const discoverData = exact<DiscoverData>()(obj(
  {
    season: seasonName, year: int, seasonal: homeSection(homeMedia), seasonTotal: int, trending: homeSection(rankedMedia),
    gems: homeSection(homeMedia), yearly: homeSection(homeMedia), schedule: homeSection(homeAiring), fetchedAt: int,
  },
  { freshness },
))
export const gemsData = exact<GemsData>()(obj({ items: arr(homeMedia) }))
export const seasonalData = exact<SeasonalData>()(obj(
  { season: seasonName, year: int, items: arr(summaryMedia), fetchedAt: int },
  { freshness },
))
export const scheduleData = exact<ScheduleData>()(obj(
  {
    airings: arr(obj(
      { anilistId: int, episode: int, airingAt: int, title: str, inLibrary: bool },
      { cover: str, format: str, posterTones: nullable(posterTones) },
    )),
    fetchedAt: int,
  },
  { freshness },
))
export const catalogSearchData = exact<CatalogSearchData>()(obj({ query: str, items: arr(summaryMedia) }))
export const episodeMetadataList = exact<EpisodeMetadata[]>()(arr(obj(
  { episode: int, title: str },
  { image: str, description: str, duration: int, airDate: str, airedAt: str },
)))
export const episodeOffset = exact<EpisodeOffset>()(obj({ known: bool, offset: int }))
export const collectionEntry = exact<CollectionEntry>()(obj(
  { anilistId: int, status: lit('watching', 'plan_to_watch', 'completed', 'dropped'), currentEpisode: int, score: nullable(int), media: summaryMedia },
  { lastWatchedAt: int },
))
export const collectionData = exact<CollectionData>()(obj({ loggedIn: bool, entries: arr(collectionEntry) }))
export const collectionDelete = exact<CollectionDeleteData>()(obj({ deleted: bool }))

// ---------- 桌面版专属：磁力 · 下载 · 来源插件 · 规则 ----------

export const torrentStatus = exact<TorrentStatus>()(obj(
  {
    active: bool, phase: lit('idle', 'metadata', 'selecting', 'buffering', 'ready'), peers: int, seeders: int,
    downRate: int, upRate: int, buffered: num, progress: num, cacheBytes: int, seeding: bool,
  },
  { name: str, fileName: str, infohash: str, error: str },
))
const torrentFile = exact<TorrentFile>()(obj({ index: int, name: str, path: str, sizeBytes: int, episode: nullable(int) }))
export const torrentPlayData = exact<TorrentPlayData>()(union(
  obj({ needSelection: lit(true), files: arr(torrentFile) }),
  obj({ needSelection: lit(false), title: str, danmaku }, { fileId: str, playbackId: str }),
))
const downloadTask = exact<DownloadTask>()(obj(
  {
    id: str, title: str, root: str, state: lit('metadata', 'downloading', 'done', 'failed'), addedAt: int,
    bytesDone: int, peers: int, seeders: int, downRate: int,
  },
  { magnet: str, torrentUrl: str, name: str, error: str, size: int, paths: arr(str), completedAt: int },
))
export const downloadsData = exact<DownloadsData>()(obj({ downloads: arr(downloadTask), dir: str }))
const pluginConfig = exact<SourcePluginConfigView>()(obj({ enabled: bool }, { executable: str, root: str }))
const manifest = exact<SourcePluginManifest>()(obj({
  id: str, name: str, version: str, protocolVersions: arr(int), sourceSchemaVersions: arr(int), capabilities: arr(str),
}))
const pluginStatus = exact<SourcePluginStatus>()(obj(
  { phase: lit('disabled', 'starting', 'ready', 'stopped', 'failed') },
  { url: str, manifest, error: str, startedAt: int },
))
const pluginSource = exact<PluginSource>()(obj({
  id: str, name: str, kind: str, tier: int, version: str, enabled: bool,
  status: lit('healthy', 'degraded', 'unavailable', 'interactive_required', 'disabled'), capabilities: arr(str),
}))
const bundled = exact<BundledPluginView>()(obj({ executable: str, root: str, active: bool }))
export const sourcePluginView = exact<SourcePluginView>()(obj(
  { config: pluginConfig, status: pluginStatus, sources: arr(pluginSource) },
  { sourcesError: str, bundled },
))
const sourceInfo = exact<SourceInfo>()(obj({
  id: str, name: str, homepage: str, enabled: bool, capabilities: obj({ seeders: bool, priority: int }), hasSelfTest: bool,
}))
const rulesInfo = exact<RulesInfo>()(obj({
  remoteUrl: str, localDir: str, dir: str, loaded: int, errors: arr(str), lastLoadedAt: nullable(int), lastSyncAt: nullable(int),
}))
export const sourcesData = exact<SourcesData>()(obj({ sources: arr(sourceInfo), rules: rulesInfo }))
