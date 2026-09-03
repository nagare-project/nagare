# homebrew-nagare

[nagare](https://github.com/nagare-project/nagare) 的 [Homebrew](https://brew.sh) tap。
一个仓库只有一件事：告诉 Homebrew 去哪儿下 nagare 的 macOS dmg、它的 SHA-256 是多少、
以及它依赖 mpv。

> 这个目录是主仓库 `nagare/packaging/homebrew/` 里的**待发布内容**，会被推成独立仓库
> `nagare-project/homebrew-nagare`。维护说明见主仓库的
> [`docs/releasing.md`](https://github.com/nagare-project/nagare/blob/main/docs/releasing.md#可选渠道homebrew-caskmacos)。

## 安装

```bash
brew tap nagare-project/nagare
brew install --cask nagare
```

（`homebrew-` 前缀由 Homebrew 自己补，所以 tap 名是 `nagare-project/nagare`。
也可以不先 tap，一条命令：`brew install --cask nagare-project/nagare/nagare`。）

装完：

- 「应用程序」里出现 `Nagare.app`，与手动下 dmg 装的是**同一个 app**。
- **mpv 一起装好了**（cask 声明了 `depends_on formula: "mpv"`），不用再单独装——
  这是走这条渠道最主要的好处，macOS 上 nagare 不捆绑 mpv。
- nagare 是**菜单栏应用**（不占 Dock），启动后浏览器自动打开界面
  （`http://127.0.0.1:8590`，只监听本机）。

系统要求 **macOS 13（Ventura）或更新**，Intel 与 Apple Silicon 通用。
更老的系统 `brew install` 会直接拒绝——产物的部署目标就钉在 13.0。

## ⚠️ brew 装的一样会被 Gatekeeper 拦一次

**先把这条说在前面：用 Homebrew 装并不能免掉系统警告。**

nagare 是零成本发布的开源项目，**没有购买 Apple 开发者证书**（$99/年）。
`Nagare.app` 只做了 **ad-hoc 签名**（`codesign -s -`），没有开发者身份、没有公证。
Homebrew 不会、也不能凭空给这个 app 加上信任——恰恰相反，cask 安装默认会给
装进去的 app **打上 quarantine 标记**，与你用浏览器下载 dmg 是一样的待遇。

所以首次打开时：

1. 打开「应用程序」里的 `Nagare`，出现「无法验证开发者」/「Apple 无法检查其是否包含
   恶意软件」时点 **「完成」**。
2. 打开 **系统设置 › 隐私与安全性**，滚到「安全性」一栏，点 **「仍要打开」**，再确认一次。

放行一次之后不再询问。macOS 15 起右键「打开」已经不能绕过这一步；始终看不到
「仍要打开」时，在终端执行：

```bash
xattr -c /Applications/Nagare.app
```

这是有意的取舍，代价写在明面上。要确认下载的文件没被动过手脚，Homebrew 自己就在做
这件事：cask 里的 `sha256` 就是那个 dmg 的哈希，对不上 brew 会直接拒绝安装。

## 用户数据在哪（升级/卸载都不会丢）

nagare 把所有状态写在**配置目录**里，不在 app 里：

| 路径 | 内容 |
| --- | --- |
| `~/Library/Application Support/nagare/config.toml` | 端口与鉴权 token（权限敏感，`0600`） |
| `~/Library/Application Support/nagare/state.json` | 媒体库、观看进度、animego 会话凭证 |
| `~/Library/Application Support/nagare/logs/` | 日志（反馈问题时附上这个） |
| `~/Library/Application Support/nagare/cache/torrent/` | 磁力分片缓存（停止播放即删，不常驻） |
| `~/Library/Application Support/nagare/update.json` | 更新检查结果缓存 |

因此：

- **升级**（`brew upgrade --cask nagare`）只换 app，用户数据原样保留。
- **卸载**（`brew uninstall --cask nagare`）同样不删这些文件。cask 的 `uninstall`
  段**故意**只做「退出进程 + 删 app」——升级和重装都会走 `uninstall`，在那里删数据
  等于静默丢观看进度。
- **要连数据一起清掉**，用显式的 zap：

  ```bash
  brew uninstall --zap --cask nagare
  ```

  `zap` 会把上面整个 `~/Library/Application Support/nagare` 连同偏好设置一起移进废纸篓。
- 例外：设了环境变量 `NAGARE_CONFIG_DIR` 的话，数据不在上面这些路径里，`zap` 清不到。

## 与 nagare 内置「一键更新」的关系

nagare 自己也有更新检查与一键自更新。**cask 装的 nagare，两条路都能走**，
但要知道它们互相看不见：

- cask 把 app 装在 `/Applications/Nagare.app`，与手动装 dmg **完全同形**。
  nagare 判断「自己是怎么装进来的」只看可执行文件路径
  （主仓库 `internal/selfupdate/channel.go`），所以它认为这是一次 dmg 安装，
  界面上的「立即更新」**是能点的**，点了会整包替换 `/Applications/Nagare.app`。
- 后果不是数据丢失（数据全在配置目录里），而是**版本记账错位**：
  brew 仍以为你装的是旧版本。
- cask 因此声明了 `auto_updates true`，把这件事如实告诉 Homebrew：
  `brew upgrade` **默认不再管 nagare**，两个更新器不会互相覆盖。

想让 brew 的记录追上应用内更新的实际版本：

```bash
brew upgrade --cask --greedy nagare
```

只想用 brew 管更新的话，在 nagare 设置页把更新检查关掉，然后定期跑上面这条 `--greedy`。

## cask 是怎么更新的

`Casks/nagare.rb` 里除了 `version` 与 `sha256` 两行，全部是**手写并且要人维护**的。
那两行由主仓库的 CI 在**每次正式发布之后**改写并推送过来
（脚本 `scripts/release/publish-homebrew-cask.sh`，哈希取自 Release 的 `checksums.txt`）。

推送时机是 GitHub 的 `release: published` 事件，**不是打 tag 的那一刻**——
nagare 的 Release 先出 draft、人工验完产物再手动发布，而 draft 的资产匿名下载一律 404。
在 draft 阶段就把 cask 推过来的话，这段时间里 `brew install --cask nagare` 会下载失败。

> 直接在这个仓库里改 `Casks/nagare.rb` 的 `version` / `sha256` 是没用的：
> 下一次发布会覆盖掉。要改 cask 的其余部分（依赖、zap 路径、caveats 文案），
> 改主仓库的 `packaging/homebrew/Casks/nagare.rb`。

预发布版本（tag 带 `-rc` / `-beta`）**不会**推到这里。这个渠道只跟稳定版。

## 边界

这个仓库里只有一个 cask，指向 nagare 官方 Release 的产物。

nagare 本体**不内置任何内容源**，源规则由用户自己提供
（见主仓库 [CONTRIBUTING.md](https://github.com/nagare-project/nagare/blob/main/CONTRIBUTING.md)
开头的四条红线）。这个 tap 同样不收录、不分发、不索引任何内容源或规则仓库地址，
相关 PR 一律不接。

## 许可证

nagare 本体是 [AGPL-3.0](https://github.com/nagare-project/nagare/blob/main/LICENSE)。
第三方组件的许可证信息见主仓库的
[`THIRD_PARTY_NOTICES.md`](https://github.com/nagare-project/nagare/blob/main/THIRD_PARTY_NOTICES.md)。
本仓库里的 cask 文件采用 [BSD-2-Clause](https://opensource.org/licenses/BSD-2-Clause)
（Homebrew tap 的惯例）。
