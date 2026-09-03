# seanime 界面对齐 —— 待办与缺口

目标：前端做到与 seanime **功能与界面一致**（漫画除外，用户 2026-09-03 定）。
后端接口缺的地方先用**假数据**跑通界面，缺口逐条记在这里。

设计数值不是估的，是从本机运行的 seanime（`127.0.0.1:43211`）实测抠出来的：

```
底色     #070707      文字 rgb(209,209,209)   muted rgba(255,255,255,.4)
描边     #ffffff1a    subtle #ffffff0f        paper #0b0b0b
品牌     #c7c2ff      圆角 .5rem              侧栏 79px
海报网格 gap 16px · 2列 → 3@768 → 4@1080 → 5@1320 → 6@1750 → 7@1850 → 8@2000
海报     6/8（3:4）· rounded .5rem · hover scale-110 · 200ms ease-out
卡片标题 16px/500，两行截断      副标题 14px/400 muted
```

---

## ⛔ 先决问题：三个入口与 nagare 自己的红线冲突

「一模一样」在这三项上做不到，**不是能力问题，是这个项目的定位问题**。
需要你拍板，我不擅自建。

| seanime 入口 | 冲突 | 依据 |
|---|---|---|
| `/onlinestream` 在线播放 | **红线 3** | 磁力是 infohash 指针；在线源直接指向盗版流。2025-04-24 两高解释把盗链入刑；美方对 aniwatch 系走 §1201 反规避一次清 939 仓 |
| `/extensions` 可执行插件 | **A3 / 红线 1** | 向用户机器投递代码执行引擎。seanime 自己 `BindFetch` 默认白名单 `["*"]`、`vm.Interrupt()` 全仓 0 调用 |
| `/debrid` Debrid 中间商 | 同类 | 调研结论：真正的打击落在 Real-Debrid 这类中间服务上 |

**建议**：这三个不做。侧栏少三个图标，换来的是本体零下架记录那条路
（Jackett/Prowlarr 引擎活、把源加回去的 fork 全 451）。

`/mediastream`（内置网页播放器 + 转码）不是红线问题，但与 A5「不做浏览器内播放、
一律交给 mpv」正面冲突。同样建议不做。

---

## 页面清单

| # | 页面 | 路由 | 状态 | 后端 |
|---|---|---|---|---|
| 1 | 首页 | `/` | ✅ 继续观看 + 海报网格 | 已有 |
| 2 | 作品详情 | `/anime/$clusterKey` | ✅ 横幅 + 剧集列表 | 已有 |
| 3 | 我的列表 | `/lists` | ⬜ 待建 | ❌ 假数据 |
| 4 | 发现 | `/discover` | ⬜ 待建 | ❌ 假数据 |
| 5 | 放送表 | `/schedule` | ⬜ 待建 | ❌ 假数据 |
| 6 | 磁力任务 | `/torrents` | ⬜ 待建 | ⚠️ 部分 |
| 7 | 扫描记录 | `/scan-summaries` | ⬜ 待建 | ❌ 假数据 |
| 8 | 自动下载 | `/auto-downloader` | ⬜ 待建 | ❌ 假数据 |
| 9 | 搜索 | `/search` | ✅ 磁力搜索 | 已有 |
| 10 | 设置 | `/settings` | ✅ | 已有 |
| — | 漫画 | — | 🚫 用户明确排除 | — |

---

## 缺口：要补的后端接口

界面先用假数据跑通，每一条都标了**假数据住在哪**，接真接口时按图索骥。

### G1 · 我的列表 `/lists`

seanime 的五档：Watching / Planning / Completed / Paused / Dropped，
数据来自 AniList 收藏。nagare 的对应物是 animego 账号。

- **要的接口**：`GET /api/lists` → 按状态分组的作品清单（标题、封面、进度 x/y）
- **animego 侧**：现在只有「进度高水位写入」（`MarkWatched`），**没有读收藏列表的接口**
- **缺口**：需要在 animego 加一个读接口，或用本地 `store.Progress` 反推
- 假数据：`frontend/src/lib/fixtures/lists.ts`

### G2 · 发现 `/discover`

seanime 是 Trending / Popular / Upcoming / 本季新番，全部来自 AniList。

- **要的接口**：`GET /api/discover?section=trending|popular|upcoming|season`
- **animego 侧**：未知是否有榜单接口，需要查
- ⚠️ **注意边界**：这是元数据（读），在允许的三条连线内。但**不得**在这里出现
  「按 anilistId 要磁力」的入口 —— 那是红线 2
- 假数据：`frontend/src/lib/fixtures/discover.ts`

### G3 · 放送表 `/schedule`

按周排的新集播出日历。

- **要的接口**：`GET /api/schedule?week=` → 每天的作品 + 集号 + 播出时间
- **animego 侧**：需要查有没有放送表数据
- 假数据：`frontend/src/lib/fixtures/schedule.ts`

### G4 · 磁力任务 `/torrents`

seanime 列出活跃种子（下载中/做种/暂停）。

- **nagare 现状**：`internal/torrentstream` 只维护【当前这一个】播放会话，
  没有「任务列表」这个概念 —— 决议 M3-4 是「停播即删分片」，本来就不留任务
- **缺口**：要么加一个真实的多任务列表（与 M3-4 冲突，要重新决议），
  要么这一页只显示当前会话
- **建议**：只显示当前会话 + 历史记录，不引入常驻下载队列
- 假数据：`frontend/src/lib/fixtures/torrents.ts`

### G5 · 扫描记录 `/scan-summaries`

每次扫描的结果存档：匹配上多少、失败多少、各是哪些文件。

- **nagare 现状**：`Rescan()` 只返回 `{videos, clusters}` 计数，**不留历史**
- **要的接口**：`GET /api/scan-summaries` → 历次扫描的详细结果
- **缺口**：需要在扫描时把结果落盘（`store` 加一张表）
- 假数据：`frontend/src/lib/fixtures/scanSummaries.ts`

### G6 · 自动下载 `/auto-downloader`

seanime 按 RSS + 规则自动下载新集。

- **nagare 现状**：完全没有。规则引擎（`internal/rules`）是**搜索**用的，不是订阅用的
- **缺口**：整套订阅 + 定时轮询 + 自动下载，是一个完整的里程碑级功能
- ⚠️ 这条要先过红线 1：源仍必须由用户提供，不得预填任何订阅地址
- 假数据：`frontend/src/lib/fixtures/autoDownloader.ts`

---

## 约定

- 假数据一律放 `frontend/src/lib/fixtures/`，**文件名与缺口编号对应**
- 每个吃假数据的 hook 顶部注释写明：`// FIXME(G3): 假数据，真接口见 todos.md`
- 接上真接口时删掉 fixture 文件，本文件对应条目打勾
- **假数据不得进生产判断逻辑** —— 只喂给界面，不参与任何决策分支
