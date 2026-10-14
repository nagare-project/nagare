# 接入 Nagare Source

Nagare 通过本机子进程接入 [Nagare Source](https://github.com/nagare-project/Nagare_Source)。来源进程统一返回 HTTP/HLS 与 BT 候选；Nagare 优先启动可靠的在线候选，播放失败时依次换源，在线候选耗尽后复用原有磁力边下边播管线。

## 安装包内置

nagare 的 dmg / NSIS 安装包 / 便携 zip / deb / rpm 都捆着一份 Nagare Source：引擎（`nagare-source`，macOS 的 .app 里按 arch 分两个）加一个**只含 BT 规则**的运行时目录 `repo/`（由 `nagare-source bundle --profile bt` 生成，里面没有 `sources/web`）。位置约定为可执行文件旁边的 `nagare-source/`（deb/rpm 为 `/usr/lib/nagare/nagare-source/`；macOS 的 .app 里引擎在 `Contents/MacOS/nagare-source-<arch>`、规则目录在 `Contents/Resources/nagare-source/repo`，bundle 规范不允许数据文件混在 MacOS/ 下）。首次运行如果从未配置过插件，nagare 自动采用这份并启用；用户改过路径或关掉之后就照用户的，设置页有「使用内置插件」可切回。

捆哪一版由 `scripts/release/source-plugin.lock` 钉死（版本 + 五平台 sha256），校验不过即构建失败；lock 未钉版本时安装包不捆插件，目录里只有一个说明文件。在线规则永远不进安装包，要用的用户自己指定完整仓库目录。

作品页的「选集」（磁力选集）也会向插件要 BT 候选：请求带 `preferences.transports: ["torrent"]`，插件只运行 BT 来源、不启动任何浏览器嗅探，通常 1–7 秒返回；结果与本机规则的结果合并，来源标为「插件 · <来源名>」，按字幕组分组。只装了插件、没有配置规则仓库的用户也能靠这条路播磁力。

插件宣告能力 `bt_releases` 时，磁力选集改走 `POST /v1/releases`：按作品的几种标题（中文名、罗马音、原名、英文名，最多四种）一次搜回整部作品的全部发布，不带集号、不按集号筛 —— 与 animego 网站的磁力搜索同一个模型。选集只在界面上排序（这一集的单集与含它的合集在前），换集不再重新请求；插件对成功的结果缓存一小时（空结果五分钟）。`GET /api/search/plugin` 的第一行 `{"event":"scope","scope":"all"}` 表示走的是这条路，`"episode"` 表示老插件按集问（那时集号参数必填）。一条不合规的发布只跳过并记在那个来源的状态里，不会中断整个结果流。

## 本地安装

克隆 Nagare Source 后，在仓库根目录构建可执行文件：

```sh
go build -o bin/nagare-source ./cmd/nagare-source
```

打开 Nagare 的“设置 → 来源”，在 **Nagare Source** 区域填写：

- **插件可执行文件**：上一步生成的 `nagare-source` 绝对路径。
- **Nagare Source 仓库目录**：包含 `schema/`、`sources/` 和 `reports/` 的仓库根目录绝对路径。

启用并保存后，状态应显示“已就绪”，下方会列出仓库公布的来源及其健康状态。作品详情页的“在线找源”按钮会按作品标题和集号请求候选。

## 从地址安装来源

不想维护整个仓库目录时，可以只装单个来源文件，或者一个**来源包**（一个文件里放一组来源）。插件宣告能力 `installed_sources`（Nagare Source 支持「独立安装目录」的版本）且已就绪时，“设置 → 来源”底部的「从地址安装的来源」会出现「从地址安装」入口；插件不支持时只显示说明，不提供入口。

安装确认页是 `http://127.0.0.1:<端口>/sources/install`，可以手动粘贴来源文件或来源包的 https 直链（SHA-256 可选），也可以由第三方页面以 `?url=<来源文件地址>&sha256=<摘要>` 的形式打开。nagare 不内置、不推荐任何来源或来源列表，链接从哪里来由用户自己决定。打开后：

1. nagare 下载文件（只走 https，连接前拒绝回环与内网地址，重定向同样检查，最大 1 MiB），算出 SHA-256；链接带了摘要时必须一致。
2. 来源包会先拆开，每个来源分别交给正在运行的插件 `POST /v1/sources/inspect` 复核格式；页面列出每个来源的名称、id、版本、类型、**会访问的域名**与文件原文（单个来源还会列出许可和上游）。与已安装或插件仓库里同 id 的来源会提示将被替换或覆盖。这一步只读，不写任何文件。
3. 用户点「安装」后，nagare 按摘要重新下载并复核，把每个来源写成 `<配置目录>/installed-sources/sources/<来源 id>.<json|yaml>`，记下出处（地址、摘要、安装时间、包含哪些来源），然后只重启一次插件；包里任何一个来源没有被插件加载，就撤销整次安装、恢复原来的文件。

来源包的格式：

```json
{
  "schema": "nagare-source-pack/v1",
  "id": "my-pack",
  "name": "我的来源包",
  "description": "可选",
  "version": "2026-10-20",
  "sources": [ { "id": "…", "kind": "bt", "…": "…" } ]
}
```

`sources` 里每一项都是一份完整的 JSON 来源文件，最多 64 个，id 不能重复。`schema` 是其他版本（如 `nagare-source-pack/v2`）时 nagare 会拒绝并提示更新；不是来源包的文件按单个来源处理。

已安装的条目在设置页逐条列出出处，来源包可以展开看包含的来源。规则如下：

- 再次安装同一个来源包就是更新：新版本不再包含的来源会一并移除，确认页会提前列出。
- 卸载按整个安装项进行，来源包会移除它的全部来源（同样会重启插件，失败则恢复）。
- 一个来源只能属于一个安装项：已经单独装过、或由另一个来源包提供的来源，需要先在设置里卸载对应项，确认页会列出冲突并禁止安装。
- 文件里标记为停用的来源不能安装。

插件通过环境变量 `NAGARE_SOURCE_INSTALLED_DIR` 读取这个目录。来源文件只是声明式的来源描述，不是可执行程序。

## 运行边界

Nagare 只启动用户明确选择的本地可执行文件，不接受远程插件地址；从地址安装的只是来源描述文件，必须经用户在确认页点击后才会保存。子进程使用系统分配的回环端口，双方协商 Plugin API v1；禁用、重新配置或退出 Nagare 时，来源进程会一起终止。

在线候选的签名 URL、Cookie 和请求头只保留在当前内存播放会话中，并直接交给 mpv。它们不会写入状态文件、候选缓存、界面或普通日志。BT 候选可以使用磁力、infohash 或 `.torrent` 地址，统一进入 Nagare 的选集、缓冲、弹幕与观看进度流程；远程种子文件有 4 MiB 上限，并拒绝访问本机或内网地址。

浏览器解析来源需要本机安装 Chrome 或 Chromium。自动探测失败时，可以先单独运行 `nagare-source serve --chrome /absolute/path/to/chrome` 检查浏览器路径和来源仓库；正式连接仍由 Nagare 管理进程与端口。
