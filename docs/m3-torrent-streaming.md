# M3 · 磁力边下边播

本文是 M3 的实现方案。磁力链接由用户提供（见 [CONTRIBUTING.md](../CONTRIBUTING.md) 的红线），
nagare 只负责把用户已有的 infohash 变成一个可以拖进度条的本地视频流，交给 mpv 播放。

引擎选型：[`anacrolix/torrent`](https://github.com/anacrolix/torrent)。
优先级窗口的常量与三处修正参考自 [seanime](https://github.com/5rahim/seanime)（GPL-3.0），已逐条在其源码中核实。

---

## 设计要点

| | 决定 | 理由 |
|---|---|---|
| **1. 复用播放管线** | 抽出 `MediaSource` 接口，磁力播放复用本地文件那条完整管线（弹幕、进度回写、看完标记） | `internal/player/manager.go` 只在三行上耦合本地路径，接缝极窄，没有理由写第二条管线 |
| **2. 做种** | 播放期间正常参与分片交换（协议必需），停止播放或退出即停止做种；设置里提供「持续做种」开关，默认关闭 | 默认行为保持最小；需要回馈 swarm 的用户可以自己开启 |
| **3. peer 发现** | 默认启用 DHT/PEX；可选地为**公开**种子补充 tracker，列表由用户配置、默认为空、不内置 | 与规则引擎「不内置任何源与 tracker」保持一致（见 `internal/rules/magnet.go`）。给私有站种子补公共 tracker 会导致封禁，因此只对公开种子生效 |
| **4. 磁盘** | 停止播放即删除该种子的分片；启动与退出各清空一次缓存目录；无容量上限 | 「启动即清空」让磁盘被写满这类故障在结构上不可能发生，且不需要 LRU 记账。缓存目录放在配置目录下的 `cache/torrent/`，不用系统临时目录（后者可能在播放中途被系统清理） |
| **5. 选集** | 复用 `internal/library` 的文件名解析链匹配集数；单候选直接播放；零候选或多候选时返回文件列表交由用户选择；过滤非视频文件与 NCOP/NCED/花絮 | 解析链已有 97 条共享语料覆盖中文字幕组命名，不再写第二套启发式 |
| **6. 端口** | 默认启用 UPnP/NAT-PMP 自动端口映射，固定默认端口（可在设置中修改） | 开箱即用的连接质量优先 |
| **7. 流端点** | 复用已有的 `/stream/{capability}` 路由，不新建鉴权机制 | 能力段每进程轮换、常数时间校验，且与主 token 分离；外部播放器只认 URL、设不了请求头，能力 URL 正是为此设计的 |
| **8. 缓冲反馈** | 分阶段状态（查找分享者 → 获取种子信息 → 缓冲进度）+ 实时 peer 数与速度，随时可取消；缓冲足够后才启动 mpv | 磁力无法像本地文件那样立即起播。不确定的等待必须有可观测的进展，否则用户无法区分「在下载」与「卡死」 |

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
| `trackers.go` | 为公开种子补充 tracker（用户配置，默认为空） |

修改：

| 文件 | 改动 |
|---|---|
| `internal/player/manager.go` | 三处接缝改为 `MediaSource` |
| `internal/player/source.go` | 新增：接口与 `LocalFileSource` |
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
| 磁盘写入 | 空间不足或无权限 | **必须有** | 「磁盘空间不足」 |
| 退出清理 | 文件被占用删不掉 | 记日志，不致命 | 无 |

---

## 不在本里程碑范围内

- **在线播放源** —— nagare 不接在线播放源，见 [CONTRIBUTING.md](../CONTRIBUTING.md)
- **内置 tracker 列表** —— 与「不内置任何源与 tracker」一致
- **容量上限的缓存池** —— 当前策略是不保留；若真有需求再评估
- **下一集预载** —— 首版不做，等真实使用反馈
- **多个种子并发播放** —— 一次只播一个，简化生命周期
- **种子文件（.torrent）导入** —— 只支持磁力链接

---

## 构建影响

`anacrolix/torrent` v1.61.0 实测可在 `CGO_ENABLED=0` 下交叉编译到
linux/amd64、windows/amd64、darwin/arm64，发布矩阵不受影响。
CI 需保留这几个目标的编译验证，以便上游升级引入 cgo 依赖时能及时发现。

二进制体积：引入该依赖后由约 9 MB 增至约 22 MB（linux/amd64，`-s -w -trimpath`）。

---

## 实现顺序

```
泳道 A: MediaSource 接缝（internal/player/）──────┐
泳道 B: torrentstream 核心 → 选集 ────────────────┼──→ API + 路由 ──→ 前端
```

A 与 B 不共享目录，可并行；合并后再做 API 层，最后接前端。
