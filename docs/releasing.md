# 发布流程

面向维护者。日常开发不需要读这篇。

nagare 零证书发布：不买代码签名证书，macOS 走 ad-hoc 签名、Windows 完全不签
（理由见 [README「为什么会有警告」](../README.md#为什么会有警告)）。代价是
**更新包的完整性完全靠 minisign 签名**，所以下面第一节是必做的一次性配置——
没做的话发出去的版本仍然能装，但客户端会拒绝自更新。

---

## 一次性配置：更新签名密钥

信任链是这样的：

```
checksums.txt.minisig --(ed25519，客户端内嵌公钥)--> checksums.txt --(sha256)--> 归档文件
```

只签 `checksums.txt` 这一份：清单里已经有每个产物的 sha256，一份签名就覆盖了
dmg / setup.exe / tar.gz / zip / .app.zip 全部产物。

### 1. 生成密钥对

```bash
brew install minisign          # 或 apt install minisign
minisign -G -W -p nagare.pub -s nagare.key
```

`-W` 生成**不加密**的私钥。这是有意的：CI 里没有人能输密码，而私钥本身由 GitHub
Secrets 保护。生成后 `nagare.key` **绝不能进版本库**，也不要留在开发机上——
放进密码管理器，本地删掉。

### 2. 公钥填进源码

`nagare.pub` 的第二行（base64 那一行）填进 [`updatekey.go`](../updatekey.go)：

```go
const updatePublicKey = "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3"
```

它是整条更新链路的信任根，**故意**放在源码里而不是 CI 变量里：改动会出现在
diff 里，是可审计的。

### 3. 私钥配进 CI

仓库 Settings → Secrets and variables → Actions → New repository secret：

| 名称 | 值 |
| --- | --- |
| `MINISIGN_SECRET_KEY` | `nagare.key` 的**完整内容**（两行，含 `untrusted comment:` 那一行） |

没配也不会阻断发布：流水线会打一条 `::warning::` 并加 `--skip=sign`，产物照发，
只是客户端自更新会因为「公钥为空 / 没有签名」而拒绝执行，退回「去下载页手动更新」。

### 密钥轮换

换密钥意味着**旧版本无法自更新到新版本**（旧客户端内嵌的是旧公钥，验不过新签名）。
只有私钥泄露时才应该轮换，轮换后要在 Release 说明里写明「本次需要手动下载安装」。

---

## 发一个版本

```bash
# 1. 先跑一次 snapshot 验流水线（不打 tag、不发布）
gh workflow run release.yml -f snapshot=true

# 2. 绿了再打 tag
git tag -a v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

tag 推上去会触发 `release.yml`，产出一个 **draft** Release。人工检查产物后再点发布。

检查清单：

- [ ] `checksums.txt` 与 `checksums.txt.minisig` 都在
- [ ] 产物齐了：`.dmg` / `.app.zip` / `-setup.exe` / Windows `.zip` / 三个 `.tar.gz` / `.deb` / `.rpm`
- [ ] dmg 里的 `Nagare.app` 能双击打开（macOS 会拦一次，点「仍要打开」）
- [ ] Windows 安装包装完托盘有图标、浏览器能打开界面
- [ ] `mpv-sources/` 在（GPL 合规：Windows 包内置 mpv 的完整对应源码）
- [ ] **点了 Publish release 之后**，`nagare-project/scoop-nagare` 的 `bucket/nagare.json`
      版本号是新的（draft 期间它指向的地址还是 404，见「可选渠道：Scoop bucket」）
- [ ] **点了 Publish release 之后**，`release.yml` 的 `homebrew` job 自动跑起来了，
      且 `nagare-project/homebrew-nagare` 的 `Casks/nagare.rb` 版本号是新的
      （这一步故意排在发布之后触发，见「可选渠道：Homebrew cask」）

### 本机验证流水线

```bash
goreleaser check
MINISIGN_KEY_FILE=$PWD/nagare.key goreleaser release --snapshot --clean --skip=publish
```

本机 cgo 环境不全时可以 `CGO_ENABLED=0` 覆盖，那样出的 `.app` 没有托盘，
但流水线其余部分照样能验。

---

## 用户怎么手动校验

Release 说明里应当保留这段：

```bash
# 校验清单本身没被篡改（公钥见 updatekey.go）
minisign -Vm checksums.txt -P '<公钥>'
# 再校验下载的文件
shasum -a 256 -c checksums.txt --ignore-missing
```

---

## 自更新的行为

客户端能不能自更新取决于**它是怎么装的**：

| 安装方式 | 自更新 |
| --- | --- |
| macOS dmg（`/Applications/Nagare.app`） | ✅ 整包替换 `.app` |
| Windows 安装包 / 便携版 zip | ✅ 替换 exe 与内置 mpv |
| Linux tar.gz | ✅ 替换二进制 |
| Linux deb / rpm（`/usr/bin`） | ❌ 由包管理器管，界面提示用 `apt` / `dnf` 升级 |
| Homebrew **cask**（`/Applications/Nagare.app`） | ⚠️ 与 dmg 同形，**认不出来**，照样整包替换——见下 |
| Scoop（Windows 应用目录） | ⚠️ 认不出来，照样替换 exe——见「可选渠道：Scoop bucket」 |

不支持时界面会说明原因并给下载页链接，不会只是按钮点不动。

`classify()`（`internal/selfupdate/channel.go`）只看可执行文件的真实路径，而
Homebrew cask 把 app 装在 `/Applications/Nagare.app` —— 与手动装 dmg 一模一样，
判出来就是 `ChannelAppBundle`。`packagePrefixes` 里的 `/opt/homebrew/` 只挡得住
**formula** 装法（bin 里的符号链接指向 Cellar），对 cask 无效。
后果不是数据丢失（数据全在配置目录），是**版本记账错位**：brew 仍以为你装的是旧版本。
cask 因此声明了 `auto_updates true`，让 `brew upgrade` 默认不去碰它，两个更新器不会打架。

### 备用分发源

Release 资产被投诉下架是同类开源项目真实发生过的事。客户端的下载地址前缀可以用
环境变量覆盖：

```bash
NAGARE_UPDATE_BASE_URL=https://example.invalid/nagare/releases/download
```

必须是 `https://`。**更新检查或下载失败绝不影响已安装版本的运行**——这是设计约束，
不是尽力而为。

---

## AUR (Arch Linux)

包名 `nagare-bin`，用官方预编译产物打包（不从源码构建）。PKGBUILD 与打包细节在
[`packaging/aur/`](../packaging/aur/README.md)。

**这个渠道不进 CI，每次发版由人手动推。** 理由写在下面，是决定，不是待办。

### 为什么不接进 CI

GoReleaser 有现成的 `aurs:` 配置能自动推 AUR，我们**刻意不用**：

1. **AUR 用 SSH 私钥推送。** 把它放进 GitHub Secrets，等于给 CI 一把能直接改发行版
   软件源的钥匙。任何能触发 workflow 的路径——被投毒的 action 依赖、一个能改
   workflow 文件的协作者、Actions 自身的漏洞——都变成「能向 Arch 用户投递任意
   PKGBUILD」的路径。
2. **PKGBUILD 是代码，不是数据。** dmg / setup.exe / tar.gz 被替换了，用户至少还有
   `checksums.txt` 那道校验；而 PKGBUILD 是**在用户机器上以用户身份执行的 shell 脚本**，
   `prepare()` / `build()` / `package()` 里写什么就跑什么。这条渠道的失陷后果比其他渠道
   高一个量级，凭证却要长期在线。
3. **爆炸半径不对称。** 本仓库被投诉下架，影响的是 nagare 自己；AUR 凭证泄露，影响的是
   别人系统上的包管理器。
4. **sha256 那一步本来就需要人。** 归档哈希要从 minisign 验过签的 `checksums.txt` 里取
   （见 `packaging/aur/README.md`）——这是一次人为的信任判断。自动化只会把它降级成
   「相信 GitHub 此刻给我的字节」，把签名链白建了。
5. **收益太小。** 这个渠道用户量小、发版频率低，手动推一次几分钟；而 AUR 的模型本来就是
   「有一个具名维护者对这个包负责」（`# Maintainer:` 那一行就是给用户看的问责线），
   自动推送把这条线抹掉了。

### 一次性：注册与配置（人工步骤，没有自动化路径）

AUR 账号要在 archlinux.org 上自己注册、邮箱激活、手动贴 SSH 公钥。
**这一段没有自动化路径**，也正因如此它天然落在某个具体的人头上：

1. 到 <https://aur.archlinux.org/register> 注册账号，收邮件激活。
2. 本机生成一把**专用**的 SSH 密钥（别复用 GitHub 那把）：

   ```bash
   ssh-keygen -t ed25519 -f ~/.ssh/aur -C "aur@nagare"
   ```

3. 登录 AUR → **My Account** → 把 `~/.ssh/aur.pub` 的内容贴进 **SSH Public Key** → 保存。
4. `~/.ssh/config` 里加一段，否则 git 会拿默认密钥去连：

   ```
   Host aur.archlinux.org
     User aur
     IdentityFile ~/.ssh/aur
     IdentitiesOnly yes
   ```

5. 克隆包仓库。**包不存在时这是正常的**，会得到一个空仓库（git 会提示
   `warning: You appear to have cloned an empty repository`），第一次 push 就等于创建：

   ```bash
   git -c init.defaultBranch=master clone ssh://aur@aur.archlinux.org/nagare-bin.git
   ```

   `init.defaultBranch=master` 不能省：**AUR 只接受推到 `master`**。本机 git 默认
   `main` 的话，克隆空仓库会建出 `main` 分支，push 直接被服务端拒。

   连不上先单独试 `ssh aur@aur.archlinux.org help`。

### 每次发版

**前提：这个版本的 GitHub Release 必须已经正式发布，不能还停在 draft。**
draft 的资产只有仓库协作者带认证才拿得到，匿名下载一律 404——AUR 用户的 `makepkg`
就是匿名下载。发版流程里 AUR 这一步**永远排在「点发布」之后**。

在 AUR 克隆里：

1. 从本仓库拷一份 `packaging/aur/PKGBUILD` 过来（或按 diff 手动同步）。
2. 改 `pkgver` 为新版本号，`pkgrel` 重置为 `1`。
   （只改打包方式、上游版本没变时：`pkgver` 不动，`pkgrel` 加一。）
3. 填 sha256：两个 Release 归档从**验过签的** `checksums.txt` 抄；
   两个仓库附件（`.desktop` / `.png`）用 `updpkgsums`（`pacman-contrib`）。
   完整命令见 [`packaging/aur/README.md`](../packaging/aur/README.md#每次发版)。
4. **重新生成 `.SRCINFO`**。它是生成物，**绝不手写**：

   ```bash
   makepkg --printsrcinfo > .SRCINFO
   ```

   ⚠️ 服务端只检查 `.SRCINFO` **存不存在**（缺了直接 `missing .SRCINFO` 拒绝推送），
   **不检查它和 PKGBUILD 对不对得上**。也就是说忘了重新生成不会报错，只会静默地
   把旧元数据发出去——网页和 `yay` / `paru` 读的都是 `.SRCINFO`，用户看到的版本号
   会停在上一版。这是 AUR 最常见的事故，而且没有任何东西会拦住你。
5. 本地验一遍：`makepkg -f` → `namcap PKGBUILD nagare-bin-*.pkg.tar.zst` →
   `sudo pacman -U nagare-bin-*.pkg.tar.zst` → `nagare -version`。
6. 提交推送：

   ```bash
   git add PKGBUILD .SRCINFO
   git commit -m "upgpkg: nagare-bin 0.2.0-1"
   git push
   ```

发完在 Release 说明或 README 里不需要额外改动——README 的 Linux 安装章节已经写了
这是社区渠道。

### 自更新

pacman 装的二进制在 `/usr/bin/nagare`，`internal/selfupdate` 按路径前缀把它判成
`ChannelPackage`，界面只提示新版本、不执行自更新（就地替换会与 pacman 的文件清单打架）。
这是既有行为，AUR 包不需要额外配置。

---

## 可选渠道：Scoop bucket（Windows）

Scoop 装的是**便携版 zip**（`nagare-<版本>_Windows_x86_64.zip`），不是 NSIS 安装包——
Scoop 的模型就是「解压到自己的目录」，不跑安装程序。zip 里 `mpv\` 与 `nagare.exe` 的
相对位置解压后不变，内置 mpv 照样能被探测到。

manifest 由 `.goreleaser.yaml` 的 `scoops:` 块在**发版时推送**到独立仓库
`nagare-project/scoop-nagare`，版本号与 SHA-256 来自它刚构建出来的那个 zip。

### 一次性：建 bucket 仓库

1. 在 `nagare-project` 组织下建一个**公开**仓库 `scoop-nagare`
   （名字随意，但 `scoop-*` 是 Scoop 的惯例；改名的话 `.goreleaser.yaml` 里
   `scoops[0].repository.name` 要跟着改）。

2. 把本仓库 [`packaging/scoop/`](../packaging/scoop/) 的内容推上去——目录结构就是
   bucket 的结构：

   ```
   README.md
   bucket/nagare.json
   ```

   `bucket/nagare.json` 里的 `hash` 是**占位符**（64 个 0），这是有意的：
   在第一次成功推送之前，`scoop install nagare` 会因为哈希对不上而**失败**，
   而不是跳过校验装一个没人验过的 exe。要让 bootstrap 立刻可用，
   在 bucket 仓库里跑一次

   ```powershell
   & "$(scoop prefix scoop)\bin\checkver.ps1" -App nagare -Dir .\bucket -Update
   ```

   它会照 manifest 里的 `checkver` / `autoupdate` 段从 GitHub Releases 取最新版本号、
   下 `checksums.txt` 取哈希，把 manifest 更到位。
   前提是**已经有一个 published（非 draft、非 pre-release）的 Release**——
   `checkver` 走的是 `/releases/latest`，只有草稿的话它什么也找不到。

3. 建 PAT 并配进**本仓库**（不是 bucket 仓库）的 secret。

### PAT 需要什么 scope

推送目标是另一个仓库，Actions 默认的 `GITHUB_TOKEN` 只能写当前仓库，所以必须用 PAT。

推荐 **fine-grained personal access token**：

| 项 | 值 |
| --- | --- |
| Resource owner | `nagare-project` |
| Repository access | Only select repositories → **勾 `scoop-nagare`**；同时接了 Homebrew 的话把 `homebrew-nagare` 也勾上（两条渠道共用 `TAP_GITHUB_TOKEN` 这一个 secret） |
| Repository permissions | **Contents: Read and write**（其余全部 No access） |
| Expiration | 自己定；**过期后发布会红**，见下方「失效时会怎样」 |

用 classic PAT 的话最小是 `public_repo`（bucket 是私有仓库才需要整个 `repo`）——
但 classic PAT 的权限覆盖账号下**所有**仓库，能用 fine-grained 就别用它。

配进 Settings → Secrets and variables → Actions → New repository secret：

| 名称 | 值 |
| --- | --- |
| `TAP_GITHUB_TOKEN` | 上面那个 PAT |

**没配也不阻断发布**：`release.yml` 会打一条 `::warning::` 并给 goreleaser 加
`--skip=scoop`，产物照发，只是 bucket 停在旧版本（Scoop 用户装到的还是上一版）。
补救办法见下方「没推成功怎么手动补」。

### 失效时会怎样

- **secret 没配**：warning + 跳过，发布仍然绿。
- **secret 配了但 PAT 过期 / 权限不对**：goreleaser 推送失败 → **整个 job 红**。
  注意此时 GitHub Release 已经建出来了（goreleaser 的 release 步骤在 scoop 之前），
  所以红的只是「bucket 没更新」，产物本身没问题——换一个有效 token，
  再照下面「没推成功怎么手动补」补一次 bucket 即可，不需要重发版本。

### 验证

发版前（不需要 PAT，也不会推任何东西）：

```bash
goreleaser check

# 跑一次上面「本机验证流水线」那条 snapshot，然后看生成的 manifest 长什么样
cat dist/scoop/bucket/nagare.json
```

CI 上 `gh workflow run release.yml -f snapshot=true` 同样会生成它，
在 `nagare-dist-full` artifact 里（snapshot 那一支**故意**不加 `--skip=scoop`，
就是为了让这份 manifest 每次都被生成出来看一眼）。

发版后，在一台 Windows 上：

```powershell
scoop bucket add nagare https://github.com/nagare-project/scoop-nagare
scoop install nagare
scoop info nagare        # 版本号、下载地址、快捷方式对不对
```

⚠️ **时序坑**：Release 是 **draft**，而 bucket 在同一次 goreleaser 运行里就被推了。
draft 的资产**下载不到**（404），所以在你手动点 Publish release 之前，
bucket 里的地址是死的。顺序永远是：**先发布 Release，再验证 `scoop install`**。

### 没推成功怎么手动补

在 bucket 仓库的 clone 里：

```powershell
& "$(scoop prefix scoop)\bin\checkver.ps1" -App nagare -Dir .\bucket -Update
git commit -am "chore: nagare <版本>"
git push
```

（`checkver.ps1` 是 Scoop 自带的脚本，本仓库不再包一层 wrapper。）

### 已知取舍：checkver / autoupdate 会被覆盖

`packaging/scoop/bucket/nagare.json` 里带了 `checkver` 与 `autoupdate` 两段，
它们是上面那条手动补的路径所依赖的。但 **GoReleaser v2.18.0 的 scoop 配置里
没有这两个字段**（实测：`goreleaser schema` 的 `Scoop` 定义里只有
name/ids/repository/directory/commit_*/homepage/description/license/url_template/
persist/skip_upload/pre_install/post_install/depends/shortcuts/goamd64），
而它生成的是一份**完整的新 manifest 并整份覆盖** `bucket/nagare.json`。

也就是说：**第一次成功推送之后，bucket 里的 checkver / autoupdate 就没了。**
需要时把 `packaging/scoop/bucket/nagare.json` 末尾那两段贴回去即可（内容不随版本变）。

如果觉得这个来回不值当，另一条路是**彻底不用 goreleaser 推**：
把 `scoops[0].skip_upload` 改成 `"true"`，bucket 仓库自己加一个定时 workflow 跑
`checkver.ps1 -Update`（用 bucket 仓库自己的 `GITHUB_TOKEN`，**不需要 PAT**），
代价是 bucket 会比 Release 晚一个轮询周期。

### 与内置一键更新的关系（已知问题）

`internal/selfupdate/channel.go` 的 `classify()` 在 Windows 上**一律返回
`ChannelDirect`**（注释写明：Windows 没有系统包管理器意义上的固定前缀）。
所以 Scoop 装的 nagare，界面上的「立即更新」**是能点的**，点了会直接改写
Scoop 应用目录里的 `nagare.exe` 与 `mpv\`。

后果不是数据丢失（用户数据全在 `%AppData%\nagare`，不在安装目录里），
而是**版本记账错位**：Scoop 仍以为装的是旧版本，下次 `scoop update nagare`
会再覆盖一遍，自更新留下的 `.old` 备份也可能残留在应用目录里。

要根治，就在 `classify()` 里把路径含 `\scoop\apps\` 的判成 `ChannelPackage`
（与 deb / rpm 同款处理，界面改成提示用 `scoop update nagare`）。
**本次没做**——那是 `internal/selfupdate` 的行为变更，要配套改测试，不属于接渠道的范围。
在此之前 bucket 的 README 里已经写明「用 Scoop 装的请走 `scoop update`」。

---

## 可选渠道：Homebrew cask（macOS）

Homebrew 装的是 **dmg 里的那个 `Nagare.app`**，与用户手动下 dmg 拖进「应用程序」
得到的是同一份字节。走这条渠道最主要的好处是 **mpv 作为依赖自动装上**——
macOS 上 nagare 不捆绑 mpv（决议 A5），手动装的用户得自己 `brew install mpv`。

cask 定义在本仓库 [`packaging/homebrew/Casks/nagare.rb`](../packaging/homebrew/Casks/nagare.rb)，
由 [`scripts/release/publish-homebrew-cask.sh`](../scripts/release/publish-homebrew-cask.sh)
在**每次正式发布之后**填好 `version` / `sha256` 推到 `nagare-project/homebrew-nagare`。

### 为什么不用 goreleaser 的 `homebrew_casks:`

Scoop 那条是 goreleaser 原生推的，Homebrew 这条不是。不是没试——是 v2.18.0 实测撞死了三条：

1. **cask pipe 只从 `archives:` 产出的归档里挑源。** dmg 与 `.app.zip` 是
   `universal_binaries` 的 post hook（`scripts/macos/build-app.sh`）生成、经
   `release.extra_files` 挂上去的，不在候选集里。喂它一个 `meta: true` 归档会直接报
   `no linux/macos archives found matching goos=[darwin linux]`。
2. **`url.template` 能改下载地址，`sha256` 改不了。** 它始终取自它挑中的那个归档——
   url 指向 dmg、哈希却是 macOS `tar.gz` 的，`brew install` 每次都因为校验不过而失败。
3. **它的 cask 模板里根本没有 `app` 这一段**，能生成的只有 `binary "nagare"`：
   装出来是一个裸二进制，没有 `.app`、没有菜单栏、没有 `LSUIElement`。

唯一能让它原生跑通的做法是把 `Nagare.app` 也塞进 macOS `tar.gz`（体积翻倍，
而且要重新验证 ad-hoc 签名扛不扛得住 goreleaser 自己的打包器）——为一个可选渠道
改主产物，不划算。所以 cask 手写，脚本只替换 `version` 与 `sha256` 两行。

### 为什么源是 dmg 而不是 `.app.zip`

两个都是 `build-app.sh` 出的、都在 Release 里、都被 `checksums.txt` 覆盖。选 dmg 因为：

- dmg 是 cask 最常规的形态，挂载与卸载由 Homebrew 自己处理，不需要额外 stanza；
- 它就是 README 让用户手动下载的那一个——**brew 装的和手动装的是同一份字节**，
  出问题时只有一条链路要查；
- `.app.zip` 留给自更新专用（`internal/selfupdate/assets.go` 按名字取它）。
  一个产物一个消费者，将来改哪边都不会牵连另一边。

### 一次性：建 tap 仓库

1. 在 `nagare-project` 组织下建一个**公开**仓库 `homebrew-nagare`。
   **名字不能随便起**：`brew tap nagare-project/nagare` 会去找
   `github.com/nagare-project/homebrew-nagare`，`homebrew-` 前缀是 Homebrew 自己补的。

2. 把本仓库 [`packaging/homebrew/`](../packaging/homebrew/) 的内容推上去——目录结构
   就是 tap 的结构：

   ```
   README.md
   Casks/nagare.rb
   ```

   推上去的 `Casks/nagare.rb` 里 `version` 是 `0.0.0`、`sha256` 是 64 个 0，
   这是**占位符**：在第一次成功推送之前 `brew install --cask nagare` 会失败
   （下载 404），而不是装一个没人验过的东西。想让 tap 立刻可用，就在本仓库里
   拿一份已发布版本的 `checksums.txt` 手工渲染一次，见下方「没推成功怎么手动补」。

3. 建 PAT 并配进**本仓库**的 secret `TAP_GITHUB_TOKEN`。

### PAT 需要什么 scope

与 Scoop bucket **共用同一个 secret** `TAP_GITHUB_TOKEN`，所以 PAT 的仓库范围里
**两个仓库都要勾上**（只勾了 `scoop-nagare` 的话，Homebrew 这一步会 403）。

推荐 **fine-grained personal access token**：

| 项 | 值 |
| --- | --- |
| Resource owner | `nagare-project` |
| Repository access | Only select repositories → **`homebrew-nagare` + `scoop-nagare`** |
| Repository permissions | **Contents: Read and write**（其余全部 No access） |
| Expiration | 自己定；过期后 `homebrew` job 会红，但那时 Release 已经发出去了 |

classic PAT 最小是 `public_repo`——但它的权限覆盖账号下**所有**仓库，能用 fine-grained
就别用它。

### 触发时机：为什么不跟着 tag

`release.yml` 里的 `homebrew` job 由 **`release: published` 事件**触发，
不是打 tag 那一刻。

打 tag 产出的是 **draft** Release，而 **draft 的资产匿名下载一律 404**——
`brew install --cask` 就是匿名下载。跟着 tag 推的话：

- 你验收产物的这段时间里，`brew install --cask nagare` 全部失败；
- 更糟的是，万一验收没过、draft 被丢掉，tap 会**永久**指着一个不存在的地址，
  直到下一次发版才自愈。

（Scoop 那条是 goreleaser 在同一次运行里推的，所以有这个 404 窗口，
文档里也标了「先发布 Release，再验证 `scoop install`」。AUR 一节给的答案同样是
「永远排在点发布之后」。Homebrew 这条直接把时机挪到发布之后，从机制上消掉了这个窗口。）

预发布（tag 带 `-rc` / `-beta`）不推：job 的 `if` 与脚本里各拦一道。

⚠️ **这个触发方式有一个静默失效点**：GitHub 不会为「用 `GITHUB_TOKEN` 做的动作」
派发新的 workflow。人在网页上点 **Publish release**、或本机
`gh release edit v0.2.0 --draft=false`（用你自己的凭证）都会触发；
但哪天把「发布草稿」这一步也写成一个用 `GITHUB_TOKEN` 的 workflow，
`homebrew` job 会**一声不响地不跑**。真要那样自动化，publish 那一步得改用 PAT。
发版检查清单里「点了 Publish 之后确认 job 跑起来了」这一条就是给这个兜底的。

### 失效时会怎样

- **secret 没配**：脚本打一条 `::warning::` 并 `exit 0`，job 绿、Release 不受影响，
  只是 tap 停在旧版本。渲染好的 cask 仍会作为 `nagare-homebrew-cask` artifact 上传，
  照下面手动补一次即可。
- **PAT 过期 / scope 里没勾这个仓库**：推送失败 → `homebrew` job 红。
  注意此时 Release **已经发布出去了**（这个 job 本来就在发布之后跑），
  红的只是「tap 没更新」，产物本身没问题。
- **模板被改坏**（`version` / `sha256` 那两行的形状变了）：脚本在渲染后会回读校验，
  发现没替换成功就直接报错退出，不会推一个还带着 `0.0.0` 的 cask 上去。

### 验证

发版前，本机（不需要 PAT，也不会推任何东西）：

```bash
# cask 是 Ruby，先过语法
ruby -c packaging/homebrew/Casks/nagare.rb

# 用 Homebrew 自己的 rubocop 规则查 stanza 顺序/分组/写法
brew style --cask packaging/homebrew/Casks/nagare.rb

# 渲染一遍看填进去的值对不对（只写 dist/，不推送）
NAGARE_CASK_DRY_RUN=1 scripts/release/publish-homebrew-cask.sh 0.2.0 dist/checksums.txt
cat dist/homebrew/Casks/nagare.rb
```

`brew audit --cask --new` 更严（会真的去下载 URL 验哈希），但它要求
Command Line Tools 是新版；跑不动时至少要跑 `brew style`。
想把 cask 真正**加载**一遍（验 stanza 名、`depends_on` 的键、artifact 类型），
可以临时建一个本地 tap：

```bash
brew tap-new nagare-check/casktest --no-git
cp dist/homebrew/Casks/nagare.rb "$(brew --repository)/Library/Taps/nagare-check/homebrew-casktest/Casks/"
brew info --cask nagare-check/casktest/nagare   # 应显示 macOS >= 13、依赖 mpv、artifact 是 App
brew untap nagare-check/casktest                 # 用完删掉
```

发版后，在一台 macOS 上：

```bash
brew tap nagare-project/nagare
brew install --cask nagare      # 会连带装 mpv
brew info --cask nagare         # 版本号、依赖、artifact 对不对
open -a Nagare                  # 首次仍会被 Gatekeeper 拦，这是预期行为
brew uninstall --cask nagare    # 只删 app，配置目录必须还在
```

配置目录（`~/Library/Application Support/nagare`）在 `uninstall` 之后**必须还在**——
那里面是观看进度与账号会话。要连数据一起清是 `brew uninstall --zap --cask nagare`。
这两条都值得在首次接通时各跑一次。

### 没推成功怎么手动补

在本仓库里渲染，然后手工提交到 tap 的 clone：

```bash
# checksums.txt 从已发布的 Release 上取（draft 取不到）
gh release download v0.2.0 --pattern checksums.txt --dir dist

NAGARE_CASK_DRY_RUN=1 scripts/release/publish-homebrew-cask.sh 0.2.0 dist/checksums.txt
# → dist/homebrew/Casks/nagare.rb

cp dist/homebrew/Casks/nagare.rb <homebrew-nagare 的 clone>/Casks/nagare.rb
# 在那个 clone 里：git commit -m "nagare 0.2.0" && git push
```

或者配好 `TAP_GITHUB_TOKEN` 后在本机直接跑（不带 `NAGARE_CASK_DRY_RUN`），
它会自己克隆、提交、推送。

### 改 cask 的哪些部分要注意

`Casks/nagare.rb` 里除了 `version` / `sha256` 两行，**全是手写、要人维护的**。
直接改 tap 仓库里的那份没用，下次发布会被覆盖——要改就改本仓库的这份。

两个容易踩的点：

- **`version` 与 `sha256` 那两行的形状是脚本的替换锚点**（行首两空格 + 关键字 + 双引号）。
  改了形状脚本会红，不会静默推错。
- **`uninstall` 里绝不能删配置目录。** 升级和重装都会走 `uninstall`，
  在那里删等于静默丢观看进度。清数据是 `zap` 的事，那是用户显式要求的动作。
