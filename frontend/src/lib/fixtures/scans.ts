// FIXME(G5/G6): 假数据。真接口缺口见仓库根 todos.md。

/** 一次扫描的存档 */
export interface FakeScan {
  id: string
  at: string
  folder: string
  videos: number
  clusters: number
  /** 解析不出集号、需要人工确认的文件 */
  unresolved: string[]
  durationMs: number
}

/** 自动下载的一条订阅规则 */
export interface FakeRule {
  id: string
  title: string
  /** 用户自填的 RSS 地址。红线 1：绝不预填任何地址，这里的示例也写成占位 */
  feed: string
  enabled: boolean
  quality: string
  lastCheckedAt: string | null
  matched: number
}

const hoursAgo = (h: number): string => new Date(Date.now() - h * 3600_000).toISOString()

export const FAKE_SCANS: FakeScan[] = [
  {
    id: 's-3',
    at: hoursAgo(2),
    folder: '/Volumes/Media/Anime',
    videos: 412,
    clusters: 38,
    unresolved: ['[Unknown] special-ova.mkv', 'bonus_disc_menu.mkv'],
    durationMs: 8_420,
  },
  {
    id: 's-2',
    at: hoursAgo(26),
    folder: '/Volumes/Media/Anime',
    videos: 388,
    clusters: 35,
    unresolved: [],
    durationMs: 7_910,
  },
  {
    id: 's-1',
    at: hoursAgo(74),
    folder: '/Users/you/Movies/Anime',
    videos: 24,
    clusters: 3,
    unresolved: ['第1话.mkv'],
    durationMs: 640,
  },
]

export const FAKE_RULES: FakeRule[] = [
  {
    id: 'r-1',
    title: '葬送的芙莉莲',
    feed: 'https://<你自己的规则源>/rss?q=Frieren',
    enabled: true,
    quality: '1080p',
    lastCheckedAt: hoursAgo(1),
    matched: 12,
  },
  {
    id: 'r-2',
    title: '迷宫饭',
    feed: 'https://<你自己的规则源>/rss?q=Dungeon+Meshi',
    enabled: false,
    quality: '1080p',
    lastCheckedAt: hoursAgo(30),
    matched: 18,
  },
]
