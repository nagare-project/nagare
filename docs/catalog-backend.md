# 目录、收藏与放送后端

所有番剧元数据经 animego 获取。图片由 nagare 登记后走本机能力 URL，预告片仍用现有 YouTube iframe。资源搜索继续通过用户配置的本机规则。

| nagare 接口 | 数据与行为 |
| --- | --- |
| `GET /api/discover` | `sections[{key,title,items,error?}], fetchedAt, freshness?`；六个板块，十分钟缓存，板块独立降级，落盘（见「离线磁盘层」） |
| `GET /api/anime/{id}` | 作品详情，包括中文简介、预告片、关联、推荐、角色和集标题；十分钟缓存，落盘，顶层可带 `freshness?` |
| `GET /api/schedule` | `airings[{anilistId,episode,airingAt,title,cover,format,inLibrary}], fetchedAt, freshness?`；三十分钟缓存，落盘 |
| `GET /api/lists` | `loggedIn, entries[{anilistId,status,currentEpisode,score,media,lastWatchedAt?}]`；账号隔离缓存一分钟 |
| `POST /api/lists/{id}` | `{status,currentEpisode,score}`，返回保存后条目；创建使用 ifAbsent |
| `DELETE /api/lists/{id}` | 删除账号收藏；返回 `{deleted:true}` |
| `/art/<cap>/remote/<key>` | 仅接受服务端登记的 SHA-256 键；最多 4096 条，按最近使用淘汰；登记表只在内存，重启后由投影重新登记（见「离线磁盘层」） |

这些 API 使用原有本机鉴权信封。Discover/Schedule 不要求 animego 登录；匿名读取 Lists 返回 200 和 `loggedIn:false`，匿名修改返回 401。

收藏状态限 watching / completed / plan_to_watch / dropped；score 为 1–10 整数或 null。减少进度前获取实际 watchedEpisodes，降序撤销目标之后的已看集数，再更新状态、评分与目标进度。中途失败返回已确认撤销、尚未确认撤销的集合与上游最后确认的进度，并立即让缓存失效。网络中断不等于最后一次写入确定失败，客户端应刷新核对。

放送保留原始 Unix 秒，由浏览器以本机时区重新分桶。上游只提供近七日；未知日期不代表停播。inLibrary 只表示本机曾建立作品绑定，不能当作逐集缺失或已看证明。

六个板块依次为：animego 在看最多、最近已播出、本季、上季、本季及下季待播、近期精选剧场版。待播筛当前/下一季度；剧场版合并季度、完结精选和年度精选的 MOVIE。类型筛选只针对已返回数据，不新增上游搜索请求。没有实现「错过的续作」和真缺集检测。

## 配套 animego 预告片字段

工作分支 `codex/catalog-trailer`，当前工作树 `/Users/lawrence_li/animego-catalog-trailer`。

1. 发布前先执行迁移 `0032_anime_trailer.up.sql`，增加 trailer_id、trailer_site 和 trailer_fetched。
2. 部署配套 Go API：季度/精选列表返回 trailerId、trailerSite；详情返回 trailer `{id,site}`。只保存有效 YouTube ID，不保存任意嵌入 URL。
3. 旧记录在访问详情时补全；已确认没有 trailer 的记录不会仅因缺预告片反复刷新。未查询 trailer 的精简列表更新保留已有值。
4. 再部署 nagare。旧季度列表缺字段时，悬停可按需读取 animego 详情补全。

以上是部署顺序，当前没有执行生产迁移或部署。回滚 Go API 后才能移除新增列，避免在运行中的新代码读写期间执行 down migration。

验证命令：nagare 的 `go vet ./...` / `go test ./...`（本机加 CGO_ENABLED=0）、前端 `bun run typecheck` / `bun run test` / `bun run build`。animego 在 go-api 下运行 `go test ./internal/anime ./internal/anilist ./internal/db/...`；Docker 与 animego-postgres:dev 就绪后运行 `go test -tags=integration ./internal/anime -run TestTrailerPostgresRoundTrip`。本机 Docker 未运行，因此真实数据库迁移测试待执行；本机 CGO 不可用，race 待可用环境验证。

作品预览、Discover 悬停卡片与作品页现已接入磁力选集：只展示已播出集数，也允许手填目标集；搜索词可编辑，资源仍只来自用户配置的本机规则。搜索页和作品入口共用一份播放会话，换页后保留缓冲与停止入口，种子内选集和重试继续携带 `episodeHint`。未配置源、源规则异常、源连接失败、零结果、引擎不可用与长时间零 peer 分开显示。首版要求用户显式选择资源，不自动猜字幕语言或把第一条结果当成最优版本。

## 海报色 `posterTones`（2026-11-01）

目录的每个作品投影（`SummaryMedia`：发现、季度、详情及其关联与推荐、搜索、收藏里的 `media`）、放送表的每一条、
媒体库的分组（`clusters[]`）与继续观看（`continueWatching[]`）都带一个 `posterTones` 字段：

```json
"posterTones": { "hue": 264.1, "tone": "#…", "quiet": "#…", "mid": "#…", "low": "#…", "onTone": "#…" }
```

- 五个颜色依次对应 `--poster-tone` / `-quiet` / `-mid` / `-low` / `--poster-on-tone`（DESIGN.md「海报色」一节），
  是**最终的 sRGB 值**：服务端按固定的 OKLCH 明度 / 彩度和封面色相算好，并做完色域映射（CSS Color 4 的二分降彩度，
  不逐通道裁剪）。客户端直接用，不自己算颜色；`hue` 只给调试和原型用。
- **字段总是存在，取不到时是 `null`**：封面还没缓存、颜色还在后台算、封面是灰的、不是 JPEG / PNG / GIF（WebP、AVIF）、
  文件损坏或超过 4000 万像素（16 位色深的 PNG 解码后内存翻倍，上限折半）。`null` 时用 `tokens.css` 里 `:root` 的中性值。没有封面（`cover` 为空）时一定是 `null`。
- 颜色只从**已经缓存在本机**的封面上取（`internal/artcache`），拼响应不发网络请求、不等解码：封面下载落盘后立刻在后台排队取色；
  响应时发现封面在、结果不在就排一次，这一次给 `null`，下一次就有颜色。同一时刻只解码一张封面。
- 结果存在封面旁边的 `<封面>.tone.json`（算法版本 + 色相或「没有颜色」的原因），每张封面只算一次；
  色阶的明度 / 彩度改了不必重算。读封面出 I/O 错误（或取色过程异常）时不写结果，十分钟后再试。
- 对比度由 `internal/artcache/ladder_test.go` 在全部 3600 个色相（接口的色相精度是 0.1°）上按最终 sRGB 值钉住：
  `tone`、`quiet` 在 `#000000` / `#1c1c1e` / `#2c2c2e` 上 ≥ 4.5:1，`mid` 在 `#000000` 上 ≥ 3:1，`onTone` 在 `tone` 上 ≥ 4.5:1。

## `GET /api/seasonal`

参数 `season`（WINTER / SPRING / SUMMER / FALL，不分大小写）与 `year`（1990–2100）。响应 `{ season, year, items: SummaryMedia[], fetchedAt, freshness? }`，items 是目录服务 `/api/anime/seasonal` 一页 200 条的投影，按 `seasonal:<季>:<年>` 缓存十分钟并合并同键在途请求；放送快照已在缓存时补 `nextAiring` / `recentAiring`。参数不合法返回 400。

## 作品搜索（2026-10-07）

`GET /api/catalog/search?q=<作品名>` → `{ query, items: SummaryMedia[] }`

- 上游是 animego `GET /api/anime/search?q=&page=1&perPage=20`。它**不在限速豁免区**（令牌桶：每 IP 每秒补 1、最多攒 60），
  没命中 animego 自己的库时还会打到 AniList，所以：
  - 前端只在提交时搜（发现页顶部搜索框，关键词进地址栏 `/discover?q=`），不边打字边搜；
  - 后端按「收拢空白 + 小写」后的关键词缓存十分钟，同键并发合并成一次出站；
  - 全局节流：两次出站至少隔 1 秒，不到点就等（排队超过 3 秒才回「搜得太快了」）；
  - 关键词最长 64 个字（按字计）。
- 只返回元数据（与其余目录读取同一份 SummaryMedia 投影），**不涉及任何资源 / 磁力**（红线 2）。

## 离线磁盘层（2026-11-01）

发现页要当第一屏，断网或目录服务连不上时也得先显示上一份。代码在 `internal/api/catalog_disk.go`（文件层）与 `catalog_cache.go`（读路径）。

- **只落公开目录**：显式白名单 `catalogDiskKey` —— 发现页六个上游板块（`trending/thisSeason/pastSeason/nextSeason/gems/yearly:<年>:<季>`）、`seasonal:<季>:<年>`、`schedule`、`detail:<id>`。**收藏不落盘**（另一个缓存实例，根本没有磁盘层；防止断网时换了账号显示上一个账号的收藏）；**搜索不落盘**（关键词是用户输入）。
- **位置与格式**：`<配置目录>/cache/catalog/<键的 SHA-256>.json`，目录 0700、文件 0600，临时文件 + 改名原子写入。文件内容 `{version, key, fetchedAt, data}`，`data` 是投影**之前**的上游结构的 JSON，读回时在 `cachedRead` 里解码成原来的类型。格式版本不符、JSON 损坏、键不符、解不成当前结构的文件：删掉并记一行日志，照常走网络，不报给界面。
- **隐私**：`detail:<id>` 文件等于记下了在本机打开过 / 关联过哪些作品（与 `cache/art` 里的封面同类），只在用户自己的配置目录里，不出本机；清空 `cache/catalog/` 即可抹掉，删掉后只是离线时没有上一份。
- **上限**：总量 50MB，按最近使用淘汰（最近使用时间同时写进文件 mtime，重启后顺序不丢）；单条超过 8MB 的不写。启动时清掉上次没写完的临时文件。
- **读路径（stale-while-revalidate）**：内存里未过期 → 直接给；内存里过期或只有盘上的一份 → 先给这一份，同时在后台刷新（与同键的其他读取合并成一次出站）；上游连不上就一直给这一份，并标记离线，一分钟之内不再重试。盘上也没有时照旧报错，板块独立降级不变。内存副本被挤掉、读取又合并进了一次失败的刷新时，退回盘上的一份（stale-if-error）。刷新成功后新的一份替换内存与盘上的旧文件。时钟回拨导致「来自未来」的副本按过期处理。
- **给界面的字段**：`/api/discover`、`/api/seasonal`、`/api/schedule` 与 `/api/anime/{id}`（顶层）在数据不是刚从上游取回时多一个 `freshness: {fetchedAt, stale, offline}`：`fetchedAt` 是 RFC 3339 的取回时间（发现页取各板块里最旧的一份）；`stale` = 超过缓存时限、后台正在刷新，稍后重新请求可拿到新的；`offline` = 最近一次刷新失败。刚取回的数据不带这个字段。原有的 `fetchedAt`（Unix 秒）不变，同样反映真实取回时间。界面显示「离线 · 更新于 …」是后续前端工作。
- **封面**：登记表（`remoteart.go`，内存 LRU 4096）不单独落盘。登记键是图片地址的 SHA-256，跨进程不变；盘上存的是原始图片地址，重启后读回来投影时照常登记回去；封面图本身已在 `internal/artcache` 的盘上，于是离线时 `/art/<能力段>/remote/<键>` 照样出图。能力段每次进程启动都换，上一个进程发出的图片地址本来就失效，界面总是先拿到新的 JSON 再请求图片，所以「先投影、后取图」这个顺序总是成立。artcache 每一跳 https 与内网拦截不受影响。
