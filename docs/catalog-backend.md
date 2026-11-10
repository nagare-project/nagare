# 目录、收藏与放送后端

所有番剧元数据经 animego 获取。图片由 nagare 登记后走本机能力 URL，预告片仍用现有 YouTube iframe。资源搜索继续通过用户配置的本机规则。

| nagare 接口 | 数据与行为 |
| --- | --- |
| `GET /api/discover` | `season, year, seasonal, seasonTotal, trending, gems, yearly, schedule, fetchedAt, freshness?`；每个板块 `{items, error?}`，十分钟缓存（放送三十分钟），板块独立降级，落盘（见「离线磁盘层」与下一节「发现页」） |
| `GET /api/discover/gems` | `{items}`：完结佳作「换一批」，每次都向上游要新的一批随机样本，不缓存、不落盘；连点只排队不并发 |
| `GET /api/anime/{id}` | 作品详情，包括中文简介、预告片、关联、推荐、角色和集标题；十分钟缓存，落盘，顶层可带 `freshness?` |
| `GET /api/schedule` | `airings[{anilistId,episode,airingAt,title,cover,format,inLibrary}], fetchedAt, freshness?`；三十分钟缓存，落盘 |
| `GET /api/lists` | `loggedIn, entries[{anilistId,status,currentEpisode,score,media,lastWatchedAt?}]`；账号隔离缓存一分钟 |
| `POST /api/lists/{id}` | `{status,currentEpisode,score}`，返回保存后条目；创建使用 ifAbsent |
| `DELETE /api/lists/{id}` | 删除账号收藏；返回 `{deleted:true}` |
| `/art/<cap>/remote/<key>` | 仅接受服务端登记的 SHA-256 键；最多 4096 条，按最近使用淘汰；登记表只在内存，重启后由投影重新登记（见「离线磁盘层」） |

这些 API 使用原有本机鉴权信封。Discover/Schedule 不要求 animego 登录；匿名读取 Lists 返回 200 和 `loggedIn:false`，匿名修改返回 401。

收藏状态限 watching / completed / plan_to_watch / dropped；score 为 1–10 整数或 null。减少进度前获取实际 watchedEpisodes，降序撤销目标之后的已看集数，再更新状态、评分与目标进度。中途失败返回已确认撤销、尚未确认撤销的集合与上游最后确认的进度，并立即让缓存失效。网络中断不等于最后一次写入确定失败，客户端应刷新核对。

放送保留原始 Unix 秒，由浏览器以本机时区重新分桶。上游只提供近七日；未知日期不代表停播。inLibrary 只表示本机曾建立作品绑定，不能当作逐集缺失或已看证明。

## 发现页 `GET /api/discover`（2026-11-10）

发现页的版面（横幅、今日更新、本季高分、按色调逛、大家都在追、完结佳作、年度榜）由前端从这一份原料拼出来：
卡片上的文字和分组有一半随时钟变（哪集已播、「现在」在哪），服务端算了前端也得重算，所以接口只给原料。

| 字段 | 内容 | 上游 |
| --- | --- | --- |
| `season`、`year` | 本季（按本机月份）与年份；年度榜也用这一年 | — |
| `seasonal`、`seasonTotal` | 本季全部，按 AniList 评分从高到低；`seasonTotal` 是这一季一共几部（与 `seasonal.items` 同数，上游一页最多 200 部） | 季度一页 200 条，键 `seasonal:<季>:<年>`，与季度页共用 |
| `trending` | 追番人数最多的十部，每项多 `rank`（从 1 起）与 `watchers` | 热门 10 条 |
| `gems` | 已完结的高分作品，随机六部 | 完结佳作 6 条 |
| `yearly` | 本年度按 AniList 评分的前十 | 年度榜 10 条 |
| `schedule` | 近七日的每一次放送 `{scheduleId, anilistId, episode, airingAt, title, cover?, hue}`，按时间先后；不按上游的日期分组，「今天」由界面按本机的日子算 | 放送表 |

每部作品是 `HomeMedia`：`anilistId, title, titleNative?, cover?, banner?, hue, score?（AniList 0–100）, bangumiScore?（0–10，未取整）, episodes, season?（WINTER…）, year?, status?, format?, genres, description?（纯文本）`。

- **`hue`** 是目录给的封面主色（`posterAccent`，`#rrggbb`）换算出的 OKLCH 色相角（一位小数），`null` = 没有可信色相：
  格式不对、彩度低于 0.04 的灰色、以及上游在取不到封面色时统一填的占位色 `#8b5cf6`（当成紫色的话，一堆不相干的作品会被归进同一个色调）。
  代码在 `internal/api/accent_hue.go`。它与作品页的 `posterTones`（本机采样封面）是两回事：发现页第一屏就要上色，等不到封面缓存下来。
  明度与彩度由界面按固定档位决定，所以无论封面是什么颜色，每一档的对比度都有下界。
- 发现页不再读上季、下季，也不再派生「待播」「剧场版」「最近已播出」三块。

旧版（版本 1）盘上的目录文件里没有封面主色、名次与追番人数，格式版本因此升到 2：升级后第一次离线启动时发现页没有上一份可显示。

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

- **只落公开目录**：显式白名单 `catalogDiskKey` —— 发现页的热门、完结佳作、年度榜（`trending/gems/yearly:<年>:<季>`）、`seasonal:<季>:<年>`（季度页与发现页的本季共用）、`schedule`、`detail:<id>`。「换一批」拿到的完结佳作不缓存也不落盘。**收藏不落盘**（另一个缓存实例，根本没有磁盘层；防止断网时换了账号显示上一个账号的收藏）；**搜索不落盘**（关键词是用户输入）。
- **位置与格式**：`<配置目录>/cache/catalog/<键的 SHA-256>.json`，目录 0700、文件 0600，临时文件 + 改名原子写入。文件内容 `{version, key, fetchedAt, data}`，`data` 是投影**之前**的上游结构的 JSON，读回时在 `cachedRead` 里解码成原来的类型。格式版本不符、JSON 损坏、键不符、解不成当前结构的文件：删掉并记一行日志，照常走网络，不报给界面。
- **隐私**：`detail:<id>` 文件等于记下了在本机打开过 / 关联过哪些作品（与 `cache/art` 里的封面同类），只在用户自己的配置目录里，不出本机；清空 `cache/catalog/` 即可抹掉，删掉后只是离线时没有上一份。
- **上限**：总量 50MB，按最近使用淘汰（最近使用时间同时写进文件 mtime，重启后顺序不丢）；单条超过 8MB 的不写。启动时清掉上次没写完的临时文件。
- **读路径（stale-while-revalidate）**：内存里未过期 → 直接给；内存里过期或只有盘上的一份 → 先给这一份，同时在后台刷新（与同键的其他读取合并成一次出站）；上游连不上就一直给这一份，并标记离线，一分钟之内不再重试。盘上也没有时照旧报错，板块独立降级不变。内存副本被挤掉、读取又合并进了一次失败的刷新时，退回盘上的一份（stale-if-error）。刷新成功后新的一份替换内存与盘上的旧文件。时钟回拨导致「来自未来」的副本按过期处理。
- **给界面的字段**：`/api/discover`、`/api/seasonal`、`/api/schedule` 与 `/api/anime/{id}`（顶层）在数据不是刚从上游取回时多一个 `freshness: {fetchedAt, stale, offline}`：`fetchedAt` 是 RFC 3339 的取回时间（发现页取各板块里最旧的一份）；`stale` = 超过缓存时限、后台正在刷新，稍后重新请求可拿到新的；`offline` = 最近一次刷新失败。刚取回的数据不带这个字段。原有的 `fetchedAt`（Unix 秒）不变，同样反映真实取回时间。界面显示「离线 · 更新于 …」是后续前端工作。
- **封面**：登记表（`remoteart.go`，内存 LRU 4096）不单独落盘。登记键是图片地址的 SHA-256，跨进程不变；盘上存的是原始图片地址，重启后读回来投影时照常登记回去；封面图本身已在 `internal/artcache` 的盘上，于是离线时 `/art/<能力段>/remote/<键>` 照样出图。能力段每次进程启动都换，上一个进程发出的图片地址本来就失效，界面总是先拿到新的 JSON 再请求图片，所以「先投影、后取图」这个顺序总是成立。artcache 每一跳 https 与内网拦截不受影响。盘上目录用到的图在封面缓存里最后才淘汰（见下一节）。

## 封面缓存的上限与清空（2026-11-01）

发现页当第一屏、一屏几百张海报，`internal/artcache`（`<配置目录>/cache/art/`）不能只增不减。代码在 `internal/artcache/index.go`（使用记录）与 `evict.go`（淘汰、清空）。

- **上限**：总量 300MB（`artcache.MaxBytes`），另有文件数上限两万张。超过任一上限就淘汰到上限的 90%（多腾一截，不至于每下一张新封面就跑一轮）。取色边车（每个几十字节）不计入总量。
- **顺序**：从最久没用的封面开始删，连同它的 `<封面>.tone.json` 一起删、内存里的取色结果一起忘掉。「用」= 经 `/art` 转发一次，或拼接口响应时用到它的海报色（浏览器按 `immutable` 缓存封面一周，显示时未必再来 `/art`，只看 `/art` 会把天天在看的海报当成没人用）。
- **在用的最后才删**：媒体库的封面（匹配结果里的 `coverUrl` 与手动认定的作品封面，`store.CoverURLs`）和盘上离线目录里出现的全部 https 图片地址（`catalogDisk.imageSources`，逐个文件扫 JSON 里的字符串，不按字段取），等别的都删完了还超才轮到，同样从最久没用的开始。artcache 不能依赖 `internal/api`（会成环），所以由 `main` 把 `Handler.PinnedArtSources` 交给 `Cache.SetPinned`；它只在后台淘汰时被调用，可以读盘，结果记 30 秒（删不掉的封面让每次下载都触发一轮淘汰时，不必每轮都重读离线目录）。回调 panic 时这一轮按都不在用处理并记日志。
- **记账**：最近使用时间记在内存索引里，并刷到 `cache/art/index.json`（临时文件 + 改名，0600；后台每分钟有变化才写一次，退出时再写一次）。不用文件系统的 atime（经常关着），也不像目录磁盘层那样改 mtime（那是 `/art` 响应的 Last-Modified，而且每出一张图就多一次写元数据）。**启动时以目录扫描为准**：索引里有、盘上没有的丢掉，盘上有、索引里没有的按 mtime 补上；索引损坏 / 版本不符 / 超过 8MB 就整个按目录重建。所以崩溃最多丢掉最后一分钟的使用顺序。启动时顺手删掉上次没写完的临时文件、空文件和没有主人的取色边车。
- **不在请求路径上**：下载落盘后发现超限只发一个信号，淘汰在后台 goroutine（`Cache.Run`）里做；它也每分钟补查一次。正在被 `/art` 转发的文件（`Cache.Open` 到 `Close`）与正在取色解码的文件不删；正在下载的还是 `.dl-*` 临时文件，不在索引里，也不删；改名到位与记账、删封面、写取色边车三件事在同一把锁里，所以不会删掉刚下好还没记账的封面，也不会留下没有主人的边车。排完序之后刚被用过的那张这一轮放过。删不掉的（Windows 上被别的程序开着）留在索引里、总量照算，下一轮再试。
- **设置页**：`GET /api/settings` 多一个 `coverCache: {bytes, count, maxBytes}`（这次运行没有封面缓存时为 `null`）；`POST /api/cover-cache/clear` 清空（变更操作，与其他 `POST` 一样要 `X-Nagare-Token` 请求头），响应 `{bytes, count, maxBytes, skipped}`，`skipped` 是那一刻正在转发、这次没删的张数。清空删掉全部封面与取色边车（包括目录里不在索引里的野文件），不动正在下载的临时文件。没有封面缓存时答 503。
