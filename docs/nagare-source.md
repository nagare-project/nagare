# 接入 Nagare Source

Nagare 通过本机子进程接入 [Nagare Source](https://github.com/nagare-project/Nagare_Source)。来源进程统一返回 HTTP/HLS 与 BT 候选；Nagare 优先启动可靠的在线候选，播放失败时依次换源，在线候选耗尽后复用原有磁力边下边播管线。

作品页的「选集」（磁力选集）也会向插件要 BT 候选：请求带 `preferences.transports: ["torrent"]`，插件只运行 BT 来源、不启动任何浏览器嗅探，通常 1–7 秒返回；结果与本机规则的结果合并，来源标为「插件 · <来源名>」，按字幕组分组。只装了插件、没有配置规则仓库的用户也能靠这条路播磁力。

## 本地安装

克隆 Nagare Source 后，在仓库根目录构建可执行文件：

```sh
go build -o bin/nagare-source ./cmd/nagare-source
```

打开 Nagare 的“设置 → 来源”，在 **Nagare Source** 区域填写：

- **插件可执行文件**：上一步生成的 `nagare-source` 绝对路径。
- **Nagare Source 仓库目录**：包含 `schema/`、`sources/` 和 `reports/` 的仓库根目录绝对路径。

启用并保存后，状态应显示“已就绪”，下方会列出仓库公布的来源及其健康状态。作品详情页的“在线找源”按钮会按作品标题和集号请求候选。

## 运行边界

Nagare 只启动用户明确选择的本地可执行文件，不接受远程插件地址。子进程使用系统分配的回环端口，双方协商 Plugin API v1；禁用、重新配置或退出 Nagare 时，来源进程会一起终止。

在线候选的签名 URL、Cookie 和请求头只保留在当前内存播放会话中，并直接交给 mpv。它们不会写入状态文件、候选缓存、界面或普通日志。BT 候选可以使用磁力、infohash 或 `.torrent` 地址，统一进入 Nagare 的选集、缓冲、弹幕与观看进度流程；远程种子文件有 4 MiB 上限，并拒绝访问本机或内网地址。

浏览器解析来源需要本机安装 Chrome 或 Chromium。自动探测失败时，可以先单独运行 `nagare-source serve --chrome /absolute/path/to/chrome` 检查浏览器路径和来源仓库；正式连接仍由 Nagare 管理进程与端口。
