# M3 · 磁力边下边播

本文是 M3 的实现说明（**已落地**）。磁力链接由用户提供（见 [CONTRIBUTING.md](../CONTRIBUTING.md) 的红线），
nagare 只负责把用户已有的 infohash 变成一个可以拖进度条的本地视频流，交给 mpv 播放。

引擎选型：[`anacrolix/torrent`](https://github.com/anacrolix/torrent)。
优先级窗口的常量与三处修正参考自 [seanime](https://github.com/5rahim/seanime)（GPL-3.0），已逐条在其源码中核实。

---

## 设计要点

| | 决定 | 理由 |
|---|---|---|
| **1. 复用播放管线** | 抽出 `MediaSource` 接口，磁力播放复用本地文件那条完整管线（弹幕、进度回写、看完标记） | `internal/player/manager.go` 只在三行上耦合本地路径，接缝极窄，没有理由写第二条管线 |
| **2. 做种** | 播放期间正常参与分片交换（协议必需），停止播放或退出即停止做种；设置里提供「持续做种」开关，默认关闭 | 默认行为保持最小；需要回馈 swarm 的用户可以自己开启。实现上映射到 `ClientConfig.Seed`：关掉**不等于**不上传 —— `PeerConn.uploadAllowed` 在非做种态仍走互惠分支（上传不超过已下载量 + 100KiB），下载停止后自然收敛到零 |
| **3. peer 发现** | 默认启用 DHT/PEX；内置一组公共 tracker（`torrentstream.DefaultTrackers`，可整组关闭）+ 用户追加，在磁力加入时即挂上 | 2026-09-12 改：索引站磁力常不带 tracker，纯 DHT 找元数据实测 50–125 秒、带 tracker 2.6 秒。tracker 是「怎么找人」的基础设施而非内容源，与 anacrolix 内置 DHT 引导节点同类。`.torrent` 文件路径上的私有种子仍一律不补 |
| **4. 磁盘** | 停止播放即删除该种子的分片；启动与退出各清空一次缓存目录；无容量上限 | 「启动即清空」让磁盘被写满这类故障在结构上不可能发生，且不需要 LRU 记账。缓存目录放在配置目录下的 `cache/torrent/`，不用系统临时目录（后者可能在播放中途被系统清理） |
| **5. 选集** | 复用 `internal/library` 的文件名解析链匹配集数；单候选直接播放；零候选或多候选时返回文件列表交由用户选择；过滤非视频文件与 NCOP/NCED/花絮 | 解析链已有 97 条共享语料覆盖中文字幕组命名，不再写第二套启发式 |
| **6. 端口** | 默认启用 UPnP/NAT-PMP 自动端口映射，固定默认端口（可在设置中修改） | 开箱即用的连接质量优先。做种/端口/端口映射都是 BT 客户端的构造期参数：**没有播放会话时就地重建客户端**（改动立即生效），正在播时不重建，如实回「重启后生效」而不是假装已生效 |
| **7. 流端点** | 复用已有的 `/stream/{capability}` 路由，不新建鉴权机制 | 能力段每进程轮换、常数时间校验，且与主 token 分离；外部播放器只认 URL、设不了请求头，能力 URL 正是为此设计的 |
| **8. 缓冲反馈** | 分阶段状态（查找分享者 → 获取种子信息 → 缓冲进度）+ 实时 peer 数与速度，随时可取消；缓冲足够后才启动 mpv | 磁力无法像本地文件那样立即起播。不确定的等待必须有可观测的进展，否则用户无法区分「在下载」与「卡死」 |
| **9. 会话终结即收** | 播放层新增 `OnSessionEnd` 回调，装配层接到引擎的停止 | 用户直接关掉 mpv 窗口是最常见的结束方式，而那条路径上不会发生任何 API 请求。没有这个回调，「停止播放即停做种」就只在点了界面按钮时才成立 |
| **10. 只走 BT swarm** | 关掉 webseed 与 WebTorrent | 磁力 URI 的 `ws=` 参数会被 BT 引擎当 webseed 直接执行 —— 那等于让用户粘进来的一行链接指使 nagare 对任意 URL 发 HTTP 请求（追踪信标、内网探测）。WebTorrent 同理不在设计范围内，开着还会牵出 ICE/STUN 的第三方出站 |
| **11. 不支持私有站种子** | 拿到种子信息时发现 private 标记就立刻中止，并说明原因 | BT 引擎没有「只对某个种子关掉 DHT/PEX」的能力，而 private 标记只存在于种子信息里 —— 磁力必须先靠 DHT 找到分享者才拿得到那份信息。等能判断时 infohash 已经进过公共 DHT，这一步无法避免；能做的是立刻停手，而不是让用户的私有站账号一直暴露在被封的风险里 |

---

## 数据流

### 播放发起

```
用户在搜索结果点「播放」
        │
        ▼
POST /api/torrent/play  {magnet, episodeHint?, fileIndex?}
        │
        ├─ 1. AddMagnet → 等待元数据    ── 超时/无 peer ─→ 「找不到可用的分享者」
        │
        ├─ 2. 选择文件
        │      ├─ 解析链解析每个文件名 → 匹配 episodeHint
        │      ├─ 单候选 ────────────────────→ 继续
        │      └─ 零候选 / 多候选 ───────────→ 200 {needSelection: true, files: [...]}
        │                                        前端弹出列表，用户选定后带 fileIndex 重发
        │
        ├─ 3. 建立优先级窗口 + 等待可播放
        │      └─ 超时 ─→ 「缓冲超时，分享者太少」
        │
        ├─ 4. 拼装流地址
        │      http://127.0.0.1:<port>/stream/<capability>/t/<infohash>/<fileIndex>
        │
        └─ 5. player.Play(ctx, TorrentSource{...})   ←── 复用完整播放管线
                     │
                     ├─ 启动 mpv，媒体路径 = 流地址
                     ├─ 弹幕：前 16MB 哈希 → 匹配 → ASS 副字幕轨
                     ├─ 进度节流回写
                     └─ 看完后标记同步
```

### 流服务

```
mpv  GET /stream/{capability}/t/{hash}/{index}   Range: bytes=12345-
        │
        ├─ 能力段常数时间校验 ─── 不符 ─→ 404（不泄露端点存在）
        ├─ 查找活动种子 ───────── 不符 ─→ 404
        ├─ 按请求区间调整分片优先级
        ├─ Reader：SetResponsive() + SetReadahead(5MB)
        └─ http.ServeContent(...)
                └─ 206 / Range / 断点续传全部交给标准库，不自行解析
```

### 优先级窗口

```
              position -= 1MB  ← 字幕簇：MKV 中字幕排在对应视频数据之前，
                    │            不回退这 1MB，一拖进度条字幕就会消失
   ┌────────┬───────┴────────┬──────────────┬───────────────┐
   │  High  │      Now       │     Next     │   Readahead   │
   │  2 片  │      5 片      │    30 片     │     30 片     │
   └────────┴────────────────┴──────────────┴───────────────┘
        ↑ 位置之前，回退容错

   启动 60 秒内额外钉住（这两段跳过降级）：
   ├─ 头 16MB  ← 弹幕匹配哈希需要前 16MB；同时也是 MKV 头部
   └─ 尾  4MB  ← MKV 的 Cues（seek 索引），顺序下载永远拿不到，
                 没有它进度条就是死的

   两个门槛要分清：起播只等头部 8MB（mpv 自己还有一层缓存），
   16MB 是钉住的目标，为的是让后台的弹幕哈希尽快算得出来 —— 起播不等它。
   尾部用的是 Next 档而不是 Now：Cues 要在第一次 seek 前到位，
   但让它和起播缓冲平级抢带宽会变成「进度条能拖、迟迟不起播」。

   位置每移动 512KB 才重算一次优先级
   优先级管理器按 torrent+file 共享，多个 reader 取并集
   （计算哈希的 reader 与 mpv 的 reader 并发存在）
```

---

## MediaSource 接缝

`internal/player/manager.go` 目前只在三处引用本地路径：存在性检查、传给 mpv 的路径、计算 16MB 哈希。
把这三处抽象掉，播放管线就不再关心媒体来自哪里。

```go
// internal/player/source.go
//
// MediaSource 把「一个可播放的东西」抽象出来，让播放管线不关心它是
// 本地文件还是磁力流。接口只有四个方法，对应原先三处直接引用本地路径的地方。
type MediaSource interface {
	// Probe 在启动 mpv 前确认媒体可用（本地：os.Stat；磁力：种子已就绪）
	Probe(ctx context.Context) error
	// MPVPath 返回交给 mpv 的路径或 URL
	MPVPath() string
	// Hash16M 返回前 16MB 的哈希，用于弹幕匹配；
	// 磁力实现会等待头部 16MB 到齐（启动期本来就钉住这一段）
	Hash16M(ctx context.Context) (string, error)
	// DisplayName 用于匹配请求的文件名参数与界面标题
	DisplayName() string
}
```

两个实现：`LocalFileSource`（包住现有的 `library.Item`，行为与今天完全一致）与
`TorrentSource`（由 `internal/torrentstream` 提供）。

配套改动：`library.Hash16M(path)` 改为 `Hash16MFrom(io.Reader)`，路径版包一层，两边共用同一实现。

---

## 文件清单

新增 `internal/torrentstream/`：

| 文件 | 职责 |
|---|---|
| `client.go` | torrent client 生命周期与配置（做种、DHT、端口映射）、启动与退出清空缓存目录 |
| `session.go` | 一次播放会话：加入磁力 → 等元数据 → 选文件 → 等就绪 → 停止即删 |
| `select.go` | 选集（复用解析链）与非视频/花絮过滤 |
| `priority.go` | 四档优先级窗口、共享优先级管理器、启动期头尾钉住 |
| `handler.go` | HTTP 处理器：Range → 调优先级 → `ServeContent` |
| `status.go` | 状态快照与实时采样（阶段、peer 数、速率、缓冲与下载进度） |
| `source.go` | `Source`：磁力流的 `MediaSource` 实现 |
| `trackers.go` | `NormalizeTrackers` + 只对公开种子补 tracker（用户配置，默认为空） |

修改：

| 文件 | 改动 |
|---|---|
| `internal/player/manager.go` | 三处接缝改为 `MediaSource`；新增 `OnSessionEnd` 回调 |
| `internal/player/source.go` | 新增：接口与本地文件实现 |
| `internal/mpv/launch.go` | 媒体路径是 HTTP 地址时跳过存在性预检 |
| `internal/library/hash.go` | `Hash16M` 改为可接受 `io.Reader` |
| `internal/httpserver/capability.go` | 流端点从占位改为转发给注册的处理器 |
| `internal/api/torrent.go` | 新增：播放 / 停止 / 状态 / 清缓存端点 |
| `internal/api/handlers.go` | 注册路由；设置载荷增加 torrent 配置与缓存占用 |
| `internal/store/store.go` | 新增 torrent 配置项 |
| `internal/errors/errors.go` | 新增磁力相关失败分类 |
| `frontend/src/` | 播放按钮、选集弹窗、磁力状态条、设置卡片 |

---

## 测试

集成测试策略：`anacrolix/torrent` 可以在同一个进程里既做种又下载，因此端到端测试是确定性的、秒级的、不联网的。

```
代码路径                                            用户流程
[+] select.go                                       [+] 点播磁力
  ├── 单集种子 → 直接选中                             ├── 单集磁力播到出画面（集成）
  ├── 合集 + episodeHint 命中                         ├── 合集弹选集 → 选完能播
  ├── 合集 + 解析不出 → 需选集                        └── 播放中拖进度条字幕不消失
  └── 全是非视频文件 → 报错
[+] priority.go                                     [+] 失败可见性
  ├── 窗口计算含 position-1MB                         ├── 无 peer → 明确提示 + 可重试
  ├── 启动期钉头 16MB / 尾 4MB                        ├── 元数据超时 → 明确原因
  ├── 60 秒后不再钉住                                 └── 磁盘写失败 → 不静默
  └── 多 reader 取并集
[+] handler.go                                      [+] 生命周期
  ├── 能力段错误 → 404                                ├── 停止播放 → 分片被删除
  ├── infohash 不匹配 → 404                           ├── 启动时清空残留目录
  ├── Range 请求 → 206                                └── 退出时种子被 drop
  └── 无 Range → 200 全量
[+] player/source.go
  ├── LocalFileSource 行为不变  ← 由现有测试覆盖（回归保护）
  └── TorrentSource.Hash16M 等待头部
```

`player.Play` 的签名变更影响所有现有调用方，因此 `internal/player` 的既有测试必须保持全绿，作为回归保护。

---

## 失败模式

| 代码路径 | 失败方式 | 处理 | 用户看到 |
|---|---|---|---|
| 等待元数据 | 无 peer，永久等待 | **必须有超时** | 「找不到可用的分享者，换一条资源试试」+ 可重试 |
| 选择文件 | 种子内全是非视频 | 有 | 「这条资源里没有可播放的视频文件」 |
| 等待可播放 | 缓冲永远到不了阈值 | **必须有超时** | 「缓冲超时，分享者太少」 |
| 计算哈希 | 头部 16MB 迟迟不到 | 弹幕降级 | 弹幕状态转为「未匹配」，**播放不受影响** |
| 流服务 | mpv 断开导致读阻塞 | ctx 取消 | 无（正常停止） |
| 磁盘写入 | 空间不足或无权限 | **必须有** | 「磁盘写入失败，已停止下载」+ 检查空间与目录权限。起播阶段由 `Prepare` 返回；播放开始之后没有请求在等着接它，因此同时写进状态快照，界面靠轮询显示 |
| 选择文件 | 种子内文件数异常 | 有 | 「这条资源的文件数异常，可能不是正常的影视发布」 |
| 等待元数据 | 是私有站种子 | 有 | 「这是私有站（PT）的种子，nagare 不支持边下边播」+ 原因 |
| 退出清理 | 文件被占用删不掉 | 记日志，不致命 | 无 |

---

## 不在本里程碑范围内

- **在线播放源** —— 只接用户显式安装的本地插件，见 [CONTRIBUTING.md](../CONTRIBUTING.md) 与 [nagare-source.md](nagare-source.md)
- ~~**内置 tracker 列表**~~ —— 2026-09-12 起内置（见上表第 3 行），红线约束的是内容源，tracker 不在其列
- **容量上限的缓存池** —— 当前策略是不保留；若真有需求再评估
- **下一集预载** —— 首版不做，等真实使用反馈
- **多个种子并发播放** —— 一次只播一个，简化生命周期
- **种子文件（.torrent）导入** —— 只支持磁力链接
- **种子内的外挂字幕** —— 只下载选中的视频文件，同一种子里独立的 `.ass/.srt` 不下载。
  中文字幕组的常见发布把字幕内嵌在 MKV 里，这条影响的主要是 BDRip 合集；有真实需求再评估
- **私有站（PT）种子** —— 见上方设计要点 11，是主动拒绝而不是尚未实现
- **webseed / WebTorrent** —— 见设计要点 10

---

## 构建影响

`anacrolix/torrent` v1.61.0 实测可在 `CGO_ENABLED=0` 下交叉编译到发布矩阵的全部五个目标
（linux amd64/arm64、windows amd64、darwin arm64/amd64），发布矩阵不受影响。
CI 需保留这几个目标的编译验证，以便上游升级引入 cgo 依赖时能及时发现。

二进制体积：引入该依赖后由约 9 MB 增至 **21 MB**（linux/amd64，`-s -w -trimpath`，实测）。

---

## 实现顺序

```
泳道 A: MediaSource 接缝（internal/player/）──────┐
泳道 B: torrentstream 核心 → 选集 ────────────────┼──→ API + 路由 ──→ 前端
```

A 与 B 不共享目录，可并行；合并后再做 API 层，最后接前端。
