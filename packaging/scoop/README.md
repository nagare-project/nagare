# scoop-nagare

[nagare](https://github.com/nagare-project/nagare) 的 [Scoop](https://scoop.sh) bucket。
一个仓库只有一件事：告诉 Scoop 去哪儿下 nagare 的 Windows 便携版、以及它的 SHA-256 是多少。

> 这个目录是主仓库 `nagare/packaging/scoop/` 里的**待发布内容**，会被推成独立仓库
> `nagare-project/scoop-nagare`。维护说明见主仓库的
> [`docs/releasing.md`](https://github.com/nagare-project/nagare/blob/main/docs/releasing.md#可选渠道scoop-bucketwindows)。

## 安装

```powershell
scoop bucket add nagare https://github.com/nagare-project/scoop-nagare
scoop install nagare
```

装完：

- 开始菜单里出现 `nagare` 快捷方式；命令行里 `nagare` 也能直接跑。
- **mpv 已内置**，不用另装（安装目录下的 `mpv\`，Windows 包一直是这样发的）。
- nagare 是 GUI 程序，**启动后只出托盘图标**，界面由浏览器自动打开
  （`http://127.0.0.1:8590`，只监听本机）。

升级与卸载：

```powershell
scoop update nagare      # 升级前先退出 nagare，否则 exe 被占用会失败
scoop uninstall nagare
```

## ⚠️ exe 没有签名，SmartScreen 可能拦

nagare 是零成本发布的开源项目，**没有购买代码签名证书**（Windows OV/EV 证书每年数百美元）。
所以：

- `nagare.exe` **完全没有数字签名**。Scoop 装完它照样没有签名——包管理器不会凭空
  给二进制加上信任。
- 首次运行时 Windows SmartScreen 可能弹「Windows 已保护你的电脑」，需要点
  **「更多信息」→「仍要运行」**。这个警告按文件哈希计算信誉，**每发一个新版本都会归零**，
  所以升级之后可能再来一次。
- 开了 **Smart App Control** 的机器会直接拦死，没有「仍要运行」可点。那种情况下
  nagare 装不了，这是零证书方案的硬边界，不是 bug。

这是有意的取舍，代价写在明面上。要确认下载的文件没被动过手脚，用 Release 附带的
`checksums.txt` 校验——Scoop 自己也做这件事：manifest 里的 `hash` 就是这个 zip 的
SHA-256，对不上 Scoop 会直接拒绝安装。

## 用户数据在哪（升级/卸载都不会丢）

nagare 把所有状态写在**配置目录**里，不在 Scoop 的安装目录里：

| 路径 | 内容 |
| --- | --- |
| `%AppData%\nagare\config.toml` | 端口与鉴权 token（权限敏感） |
| `%AppData%\nagare\state.json` | 媒体库、观看进度、animego 会话凭证 |
| `%AppData%\nagare\logs\` | 日志（反馈问题时附上这个） |
| `%AppData%\nagare\cache\torrent\` | 磁力分片缓存（停止播放即删，不常驻） |
| `%AppData%\nagare\update.json` | 更新检查结果缓存 |

因此：

- **升级**（`scoop update nagare`）只换安装目录，用户数据原样保留。
- **卸载**（`scoop uninstall nagare`）同样不删这些文件——不想要就自己删
  `%AppData%\nagare`。Scoop 的 `-p` / `--purge` 对 nagare 无效，因为 manifest 里
  没有也不需要 `persist`（persist 是给「把数据写在安装目录里」的程序用的）。
- 例外：如果你自己设了环境变量 `NAGARE_CONFIG_DIR` 指向安装目录（便携用法），
  那份数据会随 `scoop update` / `scoop uninstall` 一起没。别这么用。

## 与 nagare 内置「一键更新」的关系

nagare 自己也有更新检查与一键自更新。**用 Scoop 装的话，请用 `scoop update nagare`**：

- nagare 目前在 Windows 上不区分「怎么装进来的」，一律认为可以就地替换自己
  （见主仓库 `internal/selfupdate/channel.go`）。也就是说界面上的「立即更新」按钮
  在 Scoop 安装下**是能点的**，点了会直接改写 Scoop 应用目录里的 exe 与 `mpv\`。
- 后果不是数据丢失，而是**版本记账错位**：Scoop 仍以为你装的是旧版本，
  下一次 `scoop update nagare` 会再覆盖一遍。
- 建议在设置页关掉更新检查，或者只把它当作「有新版了」的通知，实际升级走 Scoop。

## manifest 是怎么更新的

`bucket/nagare.json` 由主仓库的 GoReleaser 在**每次发版时推送**（`.goreleaser.yaml`
的 `scoops:` 块），版本号与 SHA-256 直接来自它刚构建出来的那个 zip，不需要人工改。

仓库里这份 manifest 额外带了 `checkver` / `autoupdate` 两段，它们是 Scoop 自己的
更新机制（从 GitHub Releases 反推版本号与哈希），在这里的用途是**兜底**：
GoReleaser 的推送需要一个跨仓库 PAT，没配 / 过期时发布流水线会跳过推送，那时用

```powershell
& "$(scoop prefix scoop)\bin\checkver.ps1" -App nagare -Dir .\bucket -Update
```

就能在这个仓库里本地把 manifest 更到最新版并算好哈希。

> ⚠️ **已知取舍**：GoReleaser v2.18.0 的 scoop 配置里没有 `checkver` / `autoupdate`
> 字段（实测确认），它生成的是一份完整的新 manifest 并**整份覆盖**这个文件——
> 也就是说一次成功的发布推送之后，上面这两段会消失。需要时把它们贴回去即可
> （内容见主仓库 `packaging/scoop/bucket/nagare.json`），或者按 `docs/releasing.md`
> 的说明改成「只靠 checkver」的模式。

## 许可证

nagare 本体是 [AGPL-3.0](https://github.com/nagare-project/nagare/blob/main/LICENSE)。
Windows 包内置的 mpv 及其依赖的许可证信息见主仓库的
[`THIRD_PARTY_NOTICES.md`](https://github.com/nagare-project/nagare/blob/main/THIRD_PARTY_NOTICES.md)。
本仓库里的 manifest 文件采用 [MIT](https://opensource.org/licenses/MIT)（Scoop bucket 的惯例）。
