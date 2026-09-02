# nagare（流れ）

跑在你自己电脑上的本地动漫播放 agent：本地文件与磁力边下边播共用同一个 mpv 播放引擎，浏览器就是遥控器。

> ⚠️ **早期开发中**：M1（本地媒体库 + mpv 播放 + 弹幕）与 M2（声明式源规则）已落地——可以添加
> 本地文件夹、秒级扫描出剧集列表、调起 mpv 播放、自动匹配弹幕并回写观看进度。磁力边下边播尚未就绪；
> 三平台安装包随 [GitHub Releases](https://github.com/nagare-project/nagare/releases) 发布，见下方「安装」。

## 它是什么

- **Go 单二进制**：React UI 通过 `go:embed` 内嵌，启动后在浏览器打开 `127.0.0.1:<port>` 使用
- **两条内容来源，一个播放引擎**：本地文件、磁力边下边播（磁力链接由用户提供），解码统一交给 mpv
- **弹幕**：转成 ASS 字幕轨喂给 mpv（后续里程碑）
- **通用本地播放器**：本体零内置内容源；声明式源规则住在独立的社区仓库 nagare-rules（后续建立）

## 它不做什么

- **不解码**——视频解码由 mpv 完成，nagare 只做编排
- **不托管内容**——nagare 不提供、不分发任何媒体内容
- **不内置内容源**——源必须由用户提供，且只在用户自己的机器上执行

## 工作原理

```
┌──────────────┐   HTTP + token   ┌────────────────────┐   JSON IPC   ┌─────┐
│  浏览器 UI   │ ◄──────────────► │  nagare 本地服务   │ ◄──────────► │ mpv │
│  (React)     │                  │  (Go, 127.0.0.1)   │              └─────┘
└──────────────┘                  │  ├── 本地文件      │
                                  │  └── 磁力边下边播  │
                                  └────────────────────┘
```

浏览器里的页面只是控制界面，实际播放窗口是 mpv。本地服务只监听 `127.0.0.1`，不对局域网或公网暴露。

## 安全模型

一个跑在本机、能触发下载和拉起播放器进程的服务，鉴权必须是地基而不是补丁。M0 已实现：

| 措施 | 说明 |
| --- | --- |
| 只监听 `127.0.0.1` | 服务不暴露给局域网或公网 |
| 128 位随机 token | 首次启动生成，写入配置文件，文件权限 `0600` |
| Host 头白名单 | 只接受 `127.0.0.1:<port>` 与 `localhost:<port>`，其余一律 403，挡 DNS rebinding |
| token + 自定义头 | 所有 `/api/*` 需要 token；非 GET 请求必须把 token 放在 `X-Nagare-Token` 自定义头里——HTML 表单设不了自定义头，挡 CSRF |
| 流端点能力 URL | 随机路径、每次启动轮换；外部播放器拉流不需要携带请求头 |
| 零 CORS 头 | 服务器不发送任何 CORS 响应头，浏览器默认拒绝跨源读取 |

仓库内含这四个攻击面的验收测试：无 token 请求、伪造 Host 头、变更请求缺自定义头、伪造能力 URL。

## 安装

从 [GitHub Releases](https://github.com/nagare-project/nagare/releases) 下载对应平台的包。
nagare 没有购买代码签名证书（原因见下方「为什么会有警告」），所以首次打开时系统会拦一下，
按提示放行一次即可，之后不再询问。

### macOS

0. 系统要求：**macOS 13 或更新**（Intel 与 Apple Silicon 通用）。
1. 下载 `nagare-<版本>_MacOS_universal.dmg`，打开后把 `Nagare` 拖进「应用程序」。
2. 双击 `Nagare`。首次会提示「无法验证开发者」/「Apple 无法检查其是否包含恶意软件」——点「完成」，
   然后打开 **系统设置 › 隐私与安全性**，滚到「安全性」一栏，点 **「仍要打开」**，再确认一次。
   macOS 15 起右键「打开」已不能绕过这一步；如果没看到「仍要打开」，在终端执行
   `xattr -c /Applications/Nagare.app` 后再双击。
3. 菜单栏出现 nagare 图标（它是菜单栏应用，不占 Dock），浏览器自动打开界面。退出在菜单栏图标的菜单里。
4. **mpv 需要自行安装**：`brew install mpv`（界面里也有一键复制）。没有 mpv 时媒体库照常可用，只是不能播放。

### Windows

1. 下载 `nagare-<版本>_Windows_x86_64-setup.exe` 双击。SmartScreen 会显示「Windows 已保护你的电脑」——
   点 **「更多信息」→「仍要运行」**。
2. 安装到当前用户目录（`%LOCALAPPDATA%\Programs\nagare`），不需要管理员权限、不会弹 UAC。
3. 完成后托盘出现 nagare 图标，浏览器自动打开界面。**mpv 已内置**，不用另装。
4. 便携版：下载 `nagare-<版本>_Windows_x86_64.zip`，解压到任意目录，双击 `nagare.exe`
   （`mpv\` 子目录要和 exe 放在一起）。

### Linux

- Debian / Ubuntu：`sudo apt install ./nagare_<版本>_amd64.deb`，或双击用软件中心安装；会自动装上 mpv。
- Fedora / openSUSE：`sudo dnf install ./nagare-<版本>-1.x86_64.rpm`。
- 其他发行版：解压 `nagare-<版本>_Linux_x86_64.tar.gz`（也有 `arm64`），自行安装 mpv，运行 `./nagare`。
- 退出用界面里的「退出 nagare」，或托盘图标的菜单。

### 为什么会有警告

代码签名证书是年费制的（Apple 开发者计划 $99/年，Windows OV/EV 证书每年数百美元），nagare 是零成本
发布的开源项目，选择不买；代价就是首次打开多点一次「仍要打开 / 仍要运行」。macOS 包做了 ad-hoc
签名，Windows 包没有签名。想确认下载的文件没被动过手脚，用 Release 附带的 `checksums.txt` 校验：

```bash
shasum -a 256 -c checksums.txt --ignore-missing   # macOS / Linux，在下载目录里执行
certutil -hashfile <文件名> SHA256                  # Windows，和 checksums.txt 里的值比对
```

### 更新与日志

- **更新检查**：启动后每天最多向 GitHub 查一次最新版本号，只发出版本号、不带任何其他信息，
  可在设置里关闭。有新版时界面会提示，去 Releases 下载新包覆盖安装即可。
- **日志**：macOS `~/Library/Application Support/nagare/logs/nagare.log`、
  Windows `%AppData%\nagare\logs\`、Linux `~/.config/nagare/logs/`。反馈问题时请附上。
- **卸载**不会删除配置目录（token、媒体库状态、观看进度都在里面），不需要的话手动删。

## 从源码构建与运行

依赖：

- Go 1.25+
- [bun](https://bun.sh)（前端构建）
- mpv（播放功能的运行时依赖，M1 起接入；构建不需要）

```bash
git clone https://github.com/nagare-project/nagare.git
cd nagare

# 1. 构建前端，产物输出到仓库根的 web/
cd frontend
bun install
bun run build

# 2. 回到仓库根，构建单二进制
cd ..
go build

# 3. 运行：首次启动生成配置与 token，并自动打开浏览器
./nagare
```

开发模式（前后端分开跑）：

```bash
# 终端 1：后端，不自动打开浏览器
go run . -no-browser

# 终端 2：前端 dev server，vite 会把 /api 代理到 127.0.0.1:8590
cd frontend
bun run dev
```

测试：

```bash
go test ./...                  # 含四个鉴权攻击面的验收测试
cd frontend && bun run test
```

## 配置

| 平台 | 配置路径 |
| --- | --- |
| macOS | `~/Library/Application Support/nagare/config.toml` |
| Linux | `$XDG_CONFIG_HOME/nagare/`（默认 `~/.config/nagare/`） |
| Windows | `%AppData%\nagare\` |

- 环境变量 `NAGARE_CONFIG_DIR` 可覆盖配置目录。
- 默认端口 `8590`；被占用时自动向上寻找可用端口，并把实际使用的端口回写进配置。

## 磁力源

nagare **不内置任何磁力源**。要用磁力搜索，需要你自己提供规则：

1. 打开设置页，在「磁力源」里填规则仓库的 HTTPS 地址（或一个本机目录），点「同步规则」
2. 规则文件的格式与仓库布局见 [docs/rules-format.md](docs/rules-format.md)
3. 搜索页会对每个源单独标注状态；「源异常」表示规则解不出上游内容（多半是站点改版），
   可在设置页用规则自带的关键词做自检

规则只能声明「请求什么、怎么解」，不能执行代码；每条规则加载前都会经过格式校验。

## Roadmap

- **M0 骨架与鉴权** —— 已完成
- **M1 本地媒体库 + mpv 播放 + 弹幕** —— 已完成：目录扫描（懒哈希）、中文字幕组
  文件名解析、剧集分组聚簇、mpv JSON IPC、弹幕转 ASS（双字幕轨：对白主轨 +
  弹幕副轨）、观看进度与看完标记回写
- **M2 声明式源规则引擎** —— 已完成：磁力源由 YAML 规则描述（只能"发一个 GET + 按路径解字段"，
  无脚本无沙箱），规则从用户指定的仓库同步、校验后加载；本体零内置源。搜索结果区分
  「无结果」与「源异常（规则失效）」；规则格式见 [docs/rules-format.md](docs/rules-format.md)
- M3 磁力边下边播
- **M4 打包与分发** —— 进行中：macOS dmg（ad-hoc 签名的 universal .app）、Windows 安装包与便携版
  （内置 mpv）、Linux deb/rpm/tar.gz；零证书、零年费，安装步骤见上方「安装」
- M5 收尾

## 许可证

[AGPL-3.0](LICENSE)。

参与开发前请先读 [CONTRIBUTING.md](CONTRIBUTING.md)——尤其是开头的四条红线，它们定义了这个项目的边界。
