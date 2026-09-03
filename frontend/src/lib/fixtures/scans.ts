// FIXME(G6): 自动下载的假数据。真接口缺口见仓库根 todos.md。
//
// 这里原本还有 FAKE_SCANS（扫描记录页）。那一页连同它的假数据一起删掉了：
// 扫描信息该出现在用户扫描完【当场看的地方】，而不是一个需要他先想到去点的
// 归档页。真实的扫描丢弃现在走 GET /api/library 的 folders[].dropped。

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
