# 目录、收藏与放送后端

所有番剧元数据经 animego 获取。图片由 nagare 登记后走本机能力 URL，预告片仍用现有 YouTube iframe。资源搜索继续通过用户配置的本机规则。

| nagare 接口 | 数据与行为 |
| --- | --- |
| `GET /api/discover` | `sections[{key,title,items,error?}], fetchedAt`；六个板块，十分钟缓存，板块独立降级 |
| `GET /api/anime/{id}` | 作品详情，包括中文简介、预告片、关联、推荐、角色和集标题；十分钟缓存 |
| `GET /api/schedule` | `airings[{anilistId,episode,airingAt,title,cover,format,inLibrary}], fetchedAt`；三十分钟缓存 |
| `GET /api/lists` | `loggedIn, entries[{anilistId,status,currentEpisode,score,media,lastWatchedAt?}]`；账号隔离缓存一分钟 |
| `POST /api/lists/{id}` | `{status,currentEpisode,score}`，返回保存后条目；创建使用 ifAbsent |
| `DELETE /api/lists/{id}` | 删除账号收藏；返回 `{deleted:true}` |
| `/art/<cap>/remote/<key>` | 仅接受服务端登记的 SHA-256 键；最多 4096 条，按最近使用淘汰 |

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
