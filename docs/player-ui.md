# 播放器界面

mpv 窗口里的控制条、右键菜单、屏幕提示由 nagare 自己画，样式照方案 B「影院」：底部大区域、居中的
白色大播放键、两端带时间的粗进度条，无边框窗口由界面自己画标题与窗口按钮。配色与网页一致
（近黑表面 + 靛紫品牌色，见 `frontend/src/styles/global.css`）。

## nagare 一律用自己的 mpv 配置目录

`mpv.PrepareUIConfig` 把内置的 `internal/mpv/ui/` 同步到 `<配置目录>/runtime/mpv-config/`，起播时
用 `--config-dir` 指过去。用户自己的 `mpv.conf`、`input.conf`、脚本一概不读（2026-10-22 用户定
「始终用 nagare 界面」）。这样做还消掉了两处冲突：

- 用户配置里常见的 `autocreate-playlist`：mpv 播完会自己接同目录的下一集，而 nagare 的会话以为
  还在播第一集——下一集的位置写成上一集的进度，弹幕也还是上一集的（命令行的 `--sub-file` 对播放列表
  里每个文件都生效）。
- `save-position-on-quit`：mpv 自己记的续播位置和 nagare 传的 `--start` 打架。

每次起播都同步一遍：内容没变的不重写；被改过的改回去；旧版本留下、这一版已经没有的文件删掉
（mpv 会加载 `scripts/` 下的每一个文件）。只清理内置配置自己占的顶层路径，mpv 往配置目录里写的
其他东西不碰。准备失败不阻断播放，mpv 退回它自带的控制条。

`border=no` 与关掉 mpv 自带的控制条（`script-message osc-visibility never`）都由界面脚本在加载时做，
不写进 `mpv.conf`：万一脚本没加载起来，窗口还有系统边框，手上还有 mpv 自带的控制条。界面配置本身
没准备好时（写不出文件），nagare 带 `--no-config` 起 mpv：退回自带的控制条，但也不读用户自己的配置。

## 文件

| 文件 | 内容 |
|---|---|
| `internal/mpv/ui/mpv.conf` | 关掉 mpv 自带的进度提示与屏幕文字（`osd-level=0`，界面用自己的提示）；硬件解码 `hwdec=auto-safe`；关掉 mpv 自己的续播 |
| `internal/mpv/ui/input.conf` | 快捷键（见下表），没列出的键保留 mpv 默认 |
| `internal/mpv/ui/scripts/nagare/main.lua` | 布局、自动隐藏、鼠标与键盘、缓冲提示、屏幕提示、与 nagare 的消息 |
| `internal/mpv/ui/scripts/nagare/menu.lua` | 右键菜单与控制条按钮弹出的菜单（子菜单、悬停展开、键盘上下选择） |
| `internal/mpv/ui/scripts/nagare/draw.lua` | 圆角矩形、渐变遮罩、文字、矢量图标 → ASS 绘图指令 |
| `internal/mpv/ui.go` | 内置配置的同步 |
| `internal/player/ui.go` | 界面消息的处理、推给界面的状态、截图 |

写 Lua 时要记住的几件事：

- 不用 `utf8` 库：macOS 上的 mpv 多半配 LuaJIT（Lua 5.1），没有它。字宽靠估算（CJK 记 1em，其余 0.58em），
  ASS 没有量字宽的接口。
- ASS 绘图只会填充。图标全部画成填充形状；只要边框的地方用 `\bord` 描边、填充设成全透明
  （`Canvas:stroke_rect`）——叠一层透明形状盖不掉下面的填充。
- 左键只在光标落在按钮、进度条上（或菜单开着、正在拖）时才接管；其余时候交还 mpv，拖动窗口与双击全屏
  才照常可用。
- 坐标是 OSD 像素（`osd-dimensions`），尺寸乘 `display-hidpi-scale`。窗口逻辑宽度不到 760 时进入窄窗口
  模式，收起音量条、快退快进与倍速。

## 和 nagare 之间的消息

| 方向 | 消息 | 含义 |
|---|---|---|
| 界面 → nagare | `ready` | 脚本加载好了，要一次状态（脚本可能比 IPC 连接晚） |
| 界面 → nagare | `anime4k off\|fast\|hq` | 在播放器里换了画质增强：存进设置，套到当前窗口 |
| 界面 → nagare | `danmaku shown\|hidden` | 弹幕开关：存进设置，下一集起播照旧 |
| 界面 → nagare | `volume <0–130>` | 调完音量一秒后发：存进设置，下次起播带 `--volume` |
| 界面 → nagare | `screenshot` | nagare 建好截图目录、起好文件名再让 mpv 截（mpv 的 `screenshot-to-file` 不建目录） |
| 界面 → nagare | `prev` / `next` | 上一集 / 下一集（预留，界面在 nagare 给出 `nav` 之前不画这两个按钮） |
| nagare → 界面 | `state <json>` | `title`、`info`（第几集 · 来源）、`anime4k`；起播、弹幕编排完、网页上改了画质增强时各推一次 |
| nagare → 界面 | `toast <文字> <小字>` | 屏幕提示（截图的结果） |

界面发 `script-message nagare-event <名字> [参数]`，nagare 经 JSON IPC 收到 `client-message` 事件
（`internal/mpv/events.go`，只认 `nagare-event` 前缀）；nagare 发 `script-message-to nagare …`。

弹幕轨按标题 `nagare-danmaku` 认：有对白字幕时它挂在第二字幕轨（开关是 `secondary-sub-visibility`），
没有对白字幕时直接占主字幕轨（开关是 `sub-visibility`）。字幕菜单里不列弹幕轨。

## 快捷键

| 操作 | 键 |
|---|---|
| 播放 / 暂停 | 空格 |
| 快退 / 快进 5 秒 | ← / → |
| 音量 | ↑ / ↓ · 滚轮 |
| 静音 | M |
| 弹幕开关 | D |
| 全屏 | Enter · 双击画面 |
| 退出全屏 | Esc |
| 截图 | S |
| 窗口置顶 | T |
| 倍速 −0.25 / +0.25 / 复位 | [ / ] / Backspace |
| 右键菜单 | 右键 · Tab |
| 关闭 | Q |

## 测试

- `internal/mpv/ui_test.go`：同步、清理旧文件、改回被改过的文件、启动参数。
- `internal/mpv/integration_test.go` 的 `TestIntegration_RealMPVLoadsNagareUI`：真 mpv 用内置配置目录启动，
  触发截图快捷键，界面脚本要把消息发回来——脚本没加载起来或 Lua 报错就收不到。没装 mpv 时跳过。
- `internal/player/ui_test.go`：标题与副标题、界面消息改设置、截图文件名。
- 看样子：用 ffmpeg 造一段带章节的测试视频，`sub-add <ass> auto nagare-danmaku` 挂上弹幕，用 IPC 的
  `mouse <x> <y>` 移鼠标、`screenshot-to-file <path> window` 截带界面的窗口。`mouse` 只是模拟坐标：
  真实光标不在窗口上时，系统随后会报告鼠标离开，界面就会收起；要模拟点击，移过去 0.1 秒内就点。
