import { clusterDisplayTitle } from './associations'
import type { LibraryCluster, LibraryGroup, LibraryItem } from './endpoints'

/**
 * 按作品归组（移植自 animego「我的库」的 dedupeSeries：同一个作品 ID 的系列合并成一张卡）。
 *
 * 后端的分组（clusterKey）只看文件名：同一部番的 ANi 繁中版、LoliHouse 罗马音版、
 * 另一块盘上的副本，是三个分组。扫描后的后台识别把每个分组认到了目录作品上，
 * 这里按作品 ID 把它们并成一部；没认出作品的分组各自成一部。
 */

/** 分组对应的目录作品。confirmed：用户手动认定过（否则是自动认出的，还没确认） */
export interface ClusterWork {
  anilistId: number
  title?: string
  confirmed: boolean
}

/** 手动认定的优先；标为「不是目录里的作品」的没有作品；否则用自动认出的 */
export function clusterWork(cluster: LibraryCluster): ClusterWork | undefined {
  const { association, matched } = cluster
  if (association?.mode === 'manual' && association.anilistId) {
    return { anilistId: association.anilistId, ...(association.title ? { title: association.title } : {}), confirmed: true }
  }
  if (association !== undefined || matched === undefined) return undefined
  return { anilistId: matched.anilistId, ...(matched.title ? { title: matched.title } : {}), confirmed: false }
}

/**
 * 两个分组的季数都从文件名里认出来了、却不一样。同一部作品的两个版本不会这样 ——
 * 多半是其中一个被认错了作品（只靠文件名关键词认时会认错季），不能把它们当成同一集的不同版本。
 */
export function seasonConflict(a: LibraryCluster, b: LibraryCluster): boolean {
  return a.season !== null && b.season !== null && a.season !== b.season
}

/** 媒体库里的一部作品：一个或多个本地分组 */
export interface LibrarySeries {
  /** 列表键：认出作品的是 `a<作品 ID>`（季数对不上而单列的是 `a<作品 ID>~<clusterKey>`），没认出的是 `c<clusterKey>` */
  key: string
  work?: ClusterWork
  /** 认成了同一部作品、季数却和那部作品的其他版本对不上：单独一张海报，等用户确认 */
  ambiguous?: boolean
  title: string
  cover?: string
  /** 这部作品的全部本地分组（不同字幕组、不同文件夹），主版本在前 */
  clusters: LibraryCluster[]
  /** 本地有的集数：同一集的多个版本只算一次 */
  episodeCount: number
  season: number | null
}

/** 能当成一集播放的文件：正片、剧场版，以及没分出类型的 */
export const isPlayable = (item: LibraryItem): boolean => item.kind === 'main' || item.kind === 'movie' || item.kind === ''

const mainCount = (c: LibraryCluster) => c.groups.reduce((n, g) => n + g.items.filter(isPlayable).length, 0)

/** 版本排序：手动认定过的在前（用户确认过的那份），其次正片多的（在追的那个字幕组），其余保持原来的顺序 */
export function orderVersions(clusters: LibraryCluster[]): LibraryCluster[] {
  return clusters
    .map((cluster, index) => ({ cluster, index }))
    .sort((a, b) =>
      Number(clusterWork(b.cluster)?.confirmed ?? false) - Number(clusterWork(a.cluster)?.confirmed ?? false)
      || mainCount(b.cluster) - mainCount(a.cluster)
      || a.index - b.index)
    .map(({ cluster }) => cluster)
}

/** 一部作品里的一「集」，以及它的各个版本（版本顺序 = 分组顺序） */
interface LogicalEpisode {
  episode: number | null
  versions: { item: LibraryItem; cluster: LibraryCluster; order: number }[]
}

/**
 * 把几个分组的文件按「集」归拢：带集号的按集号；没有集号的，只有每个分组都恰好只有这一个
 * 可播放文件时（同一部剧场版的几个版本）才算同一集，否则各算一集。两边一模一样的副本只算一份。
 * 不能当成一集播放的（特典、NCOP…）放进 extras。
 */
function logicalEpisodes(clusters: LibraryCluster[]): { episodes: LogicalEpisode[]; extras: LibraryItem[]; owner: Map<string, LibraryCluster> } {
  const owner = new Map<string, LibraryCluster>()
  const perCluster = clusters.map((cluster, order) => {
    const items: LibraryItem[] = []
    for (const group of cluster.groups) {
      for (const item of group.items) {
        if (owner.has(item.fileId)) continue
        owner.set(item.fileId, cluster)
        items.push(item)
      }
    }
    return { cluster, order, items }
  })
  const singles = perCluster.every(({ items }) => {
    const playable = items.filter(isPlayable)
    return playable.length <= 1 && playable.every(item => item.episode === null)
  })
  const byKey = new Map<string, LogicalEpisode>()
  const extras: LibraryItem[] = []
  for (const { cluster, order, items } of perCluster) {
    for (const item of items) {
      if (!isPlayable(item)) {
        extras.push(item)
        continue
      }
      const key = item.episode !== null ? `ep:${item.episode}` : singles ? 'single' : `file:${item.fileId}`
      const entry = byKey.get(key) ?? { episode: item.episode, versions: [] }
      entry.versions.push({ item, cluster, order })
      byKey.set(key, entry)
    }
  }
  const episodes = [...byKey.values()].sort((a, b) => (a.episode ?? Infinity) - (b.episode ?? Infinity))
  return { episodes, extras, owner }
}

/** 本地集数：同一集的多个版本只算一次；一集可播放的都没有时数全部文件 */
export function localEpisodeCount(clusters: LibraryCluster[]): number {
  const { episodes, extras } = logicalEpisodes(clusters)
  return episodes.length || extras.length
}

/** 一部作品的观看进度，按集计：同一集的几个版本里看完任意一个就算看完这一集 */
export interface SeriesProgress {
  /** 集数（同 localEpisodeCount） */
  total: number
  /** 看完的集数 */
  completed: number
  /** 看过（看完或看了一部分）的集数 */
  started: number
}

export function seriesProgress(clusters: LibraryCluster[]): SeriesProgress {
  const { episodes, extras } = logicalEpisodes(clusters)
  // 一集可播放的都没有（全是特典一类）时按文件算
  const units = episodes.length > 0 ? episodes.map(e => e.versions.map(v => v.item)) : extras.map(item => [item])
  const done = (item: LibraryItem) => item.progress?.completed === true
  const begun = (item: LibraryItem) => done(item) || (item.progress?.positionSec ?? 0) > 0
  return {
    total: units.length,
    completed: units.filter(versions => versions.some(done)).length,
    started: units.filter(versions => versions.some(begun)).length,
  }
}

/** 把分组按作品归成系列，保持第一次出现的位置 */
export function groupSeries(clusters: LibraryCluster[]): LibrarySeries[] {
  const out: LibrarySeries[] = []
  const byWork = new Map<number, LibrarySeries>()
  for (const cluster of clusters) {
    const work = clusterWork(cluster)
    const existing = work ? byWork.get(work.anilistId) : undefined
    if (existing && !seasonConflict(existing.clusters[0]!, cluster)) {
      existing.clusters.push(cluster)
      continue
    }
    const ambiguous = existing !== undefined
    const series: LibrarySeries = {
      key: work ? `a${work.anilistId}${ambiguous ? `~${cluster.clusterKey}` : ''}` : `c${cluster.clusterKey}`,
      ...(work ? { work } : {}),
      ...(ambiguous ? { ambiguous } : {}),
      title: clusterDisplayTitle(cluster),
      clusters: [cluster],
      episodeCount: 0,
      season: cluster.season,
    }
    if (work && !ambiguous) byWork.set(work.anilistId, series)
    out.push(series)
  }
  return out.map(series => {
    const ordered = orderVersions(series.clusters)
    const works = ordered.map(clusterWork).filter((w): w is ClusterWork => w !== undefined)
    const work = works.find(w => w.confirmed) ?? works[0]
    const cover = ordered.find(c => c.cover !== undefined)?.cover
    return {
      ...series,
      ...(work ? { work } : {}),
      title: work?.title || clusterDisplayTitle(ordered[0]!),
      ...(cover === undefined ? {} : { cover }),
      clusters: ordered,
      episodeCount: localEpisodeCount(ordered),
      season: ordered[0]!.season,
    }
  })
}

/** 同一集的另一个版本，以及界面上用来区分它的短名 */
export interface LibraryVersion {
  item: LibraryItem
  label: string
}

/** 合并后的剧集列表：每一集一个主版本，其余版本挂在它下面 */
export interface MergedSeries {
  /** 拼出来的一个分组：每一集一个主版本，附加内容另成一组。clusterKey 是主版本分组的 */
  cluster: LibraryCluster
  /** 主版本 fileId → 同一集的其他版本 */
  versions: Map<string, LibraryVersion[]>
  /** fileId → 它所在的本地分组（播放前要按它认定作品） */
  owner: Map<string, LibraryCluster>
}

const RESOLUTION_RANK: Record<string, number> = { '2160p': 4, '4k': 4, '1080p': 3, '720p': 2, '480p': 1 }
const resolutionRank = (item: LibraryItem) => RESOLUTION_RANK[(item.resolution ?? '').toLowerCase()] ?? 0

/** 同一集的版本里挑主版本：正在看的 > 看完的 > 分组靠前的（见 orderVersions）> 分辨率高的 */
function versionScore(item: LibraryItem): number {
  if (item.progress && item.progress.positionSec > 0 && !item.progress.completed) return 2
  return item.progress?.completed ? 1 : 0
}

/** 版本的短名：字幕组（文件名里没有就用分组的文件夹标题）· 分辨率 */
export function versionLabel(item: LibraryItem, cluster?: LibraryCluster): string {
  return [item.group || cluster?.title, item.resolution].filter(Boolean).join(' · ') || item.fileName
}

/**
 * 把一部作品的多个本地分组并成一份剧集列表。只有一个分组时原样返回（不拼）：
 * 单个分组按目录分区显示的既有版式不变。
 */
export function mergeSeries(clusters: LibraryCluster[]): MergedSeries {
  if (clusters.length === 1) {
    const only = clusters[0]!
    return { cluster: only, versions: new Map(), owner: logicalEpisodes([only]).owner }
  }
  const ordered = orderVersions(clusters)
  const { episodes, extras, owner } = logicalEpisodes(ordered)
  const versions = new Map<string, LibraryVersion[]>()
  const main: LibraryItem[] = []
  for (const { versions: candidates } of episodes) {
    const [primary, ...rest] = [...candidates].sort((a, b) =>
      versionScore(b.item) - versionScore(a.item) || a.order - b.order || resolutionRank(b.item) - resolutionRank(a.item))
    main.push(primary!.item)
    if (rest.length > 0) versions.set(primary!.item.fileId, rest.map(r => ({ item: r.item, label: versionLabel(r.item, r.cluster) })))
  }
  const groups: LibraryGroup[] = [{ groupKey: '__series__', label: '', sortMode: 'episode', items: main }]
  if (extras.length > 0) groups.push({ groupKey: '__series_extras__', label: '特典与其他', sortMode: 'alpha', items: extras })
  const first = ordered[0]!
  const cover = ordered.find(c => c.cover !== undefined)?.cover
  const cluster: LibraryCluster = {
    ...first,
    episodeCount: episodes.length || extras.length,
    ...(cover === undefined ? {} : { cover }),
    groups,
  }
  return { cluster, versions, owner }
}
