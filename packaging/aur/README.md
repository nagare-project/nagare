# AUR 打包（`nagare-bin`）

Arch Linux 用户装 nagare 的渠道。**社区渠道，不由 CI 产出**——理由见
[docs/releasing.md「AUR」](../../docs/releasing.md#aur-arch-linux)。

## 这个目录是什么

这里是 `PKGBUILD` 的**上游副本**，不是 AUR 仓库本身。AUR 仓库是另一个独立的 git 仓库：

```
ssh://aur@aur.archlinux.org/nagare-bin.git
```

副本放在本仓库里，是为了让 `PKGBUILD` 和它引用的两个文件
（[`../linux/nagare.desktop`](../linux/nagare.desktop)、[`../icon/nagare-256.png`](../icon/nagare-256.png)）
落在同一个 diff 里：改了 desktop 文件、挪了图标路径，AUR 侧要跟着动这件事才看得见。

真正发布时把 `PKGBUILD` 拷进 AUR 克隆，改完 `pkgver`/sha256，再生成 `.SRCINFO` 提交。

**AUR 仓库里只有两个文件**：`PKGBUILD` 和 `.SRCINFO`。这不是洁癖，是服务端限制：
单个 blob 上限 250 KiB（所以 25 KB 的图标虽然塞得下，构建产物和源码归档一律塞不进去），
而且**除 `keys/` 与 `LICENSES/` 外不允许任何子目录**。这也是 `.desktop` 与图标必须走
URL 下载、而不是跟着 PKGBUILD 一起提交的原因之一。

## 为什么这里没有 `.SRCINFO`

AUR 仓库里必须有 `.SRCINFO`——服务端 pre-receive 钩子看不到它就直接
`missing .SRCINFO` 拒绝推送。但它是**生成物**，唯一正确的产生方式是：

```bash
makepkg --printsrcinfo > .SRCINFO
```

⚠️ 服务端只检查它**在不在**，**不检查它和 PKGBUILD 对不对得上**。所以一份生成物存两个
地方的代价不是「推不上去」，而是「悄悄发出错的元数据」——没有任何东西会拦住你。
本目录因此**故意不放** `.SRCINFO`，它只在 AUR 克隆里生成、提交。**永远不要手写它。**

在没有 Arch 的机器上（比如 macOS）可以用容器生成，`docker` 换 `podman` 一样：

```bash
# 在 AUR 克隆的目录里执行
podman run --rm -v "$PWD:/pkg" -w /pkg docker.io/library/archlinux:base-devel bash -c '
  useradd -m builder && chown -R builder /pkg &&
  su builder -c "makepkg --printsrcinfo > .SRCINFO"'
```

（makepkg 拒绝以 root 身份运行，所以容器里要建一个普通用户。）

## 首次上架

一次性动作，见 [docs/releasing.md](../../docs/releasing.md#aur-arch-linux)：注册 AUR 账号、
传 SSH 公钥、`git clone ssh://aur@aur.archlinux.org/nagare-bin.git`。

## 每次发版

**前提：对应版本的 GitHub Release 必须已经正式发布，不能还是 draft。**
draft Release 的资产只有仓库协作者用带认证的 API 才拿得到，匿名下载一律 404 ——
AUR 用户的 `makepkg` 就是匿名下载。

1. **改 `pkgver`**，`pkgrel` 重置为 `1`。
   （只改打包方式、版本号没变时：`pkgver` 不动，`pkgrel` 加一。）

2. **填 sha256。** 两类来源分开处理：

   - **两个 Release 归档**（`sha256sums_x86_64` / `sha256sums_aarch64`）：
     从 Release 里那份 **minisign 验过签的** `checksums.txt` 抄，别只信 `updpkgsums`。
     `updpkgsums` 只是「把 GitHub 现在给我的字节哈希一遍」，它证明不了这些字节是我们发的：

     ```bash
     curl -LO https://github.com/nagare-project/nagare/releases/download/v0.2.0/checksums.txt
     curl -LO https://github.com/nagare-project/nagare/releases/download/v0.2.0/checksums.txt.minisig
     # 公钥见仓库根的 updatekey.go
     minisign -Vm checksums.txt -P '<公钥>'
     grep Linux checksums.txt
     ```

   - **两个仓库附件**（`sha256sums`，desktop 与 png）：不在 `checksums.txt` 里，
     用 `updpkgsums`（`pacman-contrib` 包）或手动 `sha256sum` 填。

   关于 `updpkgsums` 的两个实测行为（v0.2.0 上验过）：
   它会**一次性重写全部三个 `sha256sums*` 数组**（把它们整块删掉、在第一个数组原来的
   位置重新打印），本文件里的注释都在数组之前，所以排版和注释不会被打乱；
   而且它会为 `arch=()` 里的**每一个**架构生成校验和，也就是**两个归档都要下载**，
   在只有 x86_64 机器的情况下也一样。

   两条路都走完之后，跑一次 `updpkgsums` 交叉验证是好习惯：它算出来的值应当与
   `checksums.txt` 里的逐字相同。**不同就停下来查，别改 PKGBUILD 去将就它。**

3. **重新生成 `.SRCINFO`**（见上）。忘了这步是 AUR 最常见的事故：
   AUR 网页与 `yay`/`paru` 读的是 `.SRCINFO`，它还停在旧版本，用户就会下到旧归档、
   或者对着新归档校验旧 sha256 而失败。

4. **本地验一遍**（Arch 机器或容器里）：

   ```bash
   makepkg -f           # 下载 + 校验 + 打包
   namcap PKGBUILD
   namcap nagare-bin-*.pkg.tar.zst
   sudo pacman -U nagare-bin-*.pkg.tar.zst
   nagare -version      # 版本号对得上
   nagare -no-browser   # 起得来；界面里看 mpv 是否被探测到，Ctrl-C 退出
   ```

   手头没有 Arch 机器时（比如 macOS），整套在容器里跑一遍就够——写这个包时就是这么验的：

   ```bash
   # 在 AUR 克隆的目录里执行；Apple Silicon 上靠 --platform 走 x86_64 模拟，
   # 本包不编译任何东西，模拟的开销可以忽略。
   docker run --rm --platform linux/amd64 -v "$PWD:/work:ro" -w /work \
     archlinux:base-devel bash -c '
       pacman -Sy --noconfirm --needed namcap desktop-file-utils
       useradd -m builder && cp -r /work /home/builder/pkg &&
       chown -R builder /home/builder/pkg && cd /home/builder/pkg
       su builder -c "makepkg -f --noconfirm"
       namcap PKGBUILD; namcap ./*.pkg.tar.zst
       pacman -U --noconfirm ./*.pkg.tar.zst && /usr/bin/nagare -version'
   ```

5. **提交并推送**。**AUR 只接受推到 `master` 分支**——本机 git 默认 `main` 的话
   （克隆空仓库时没带 `-c init.defaultBranch=master`），push 会被服务端直接拒掉：

   ```bash
   git add PKGBUILD .SRCINFO
   git commit -m "upgpkg: nagare-bin 0.2.0-1"
   git push
   ```

## namcap 的四条告警是预期的，别去「修」

v0.2.0 上在 `archlinux:base-devel` 容器里实测过，干净构建后剩下这四条：

```
PKGBUILD (nagare-bin) W: Reference to x86_64 should be changed to $CARCH
nagare-bin W: ELF file ('usr/bin/nagare') lacks FULL RELRO, check LDFLAGS.
nagare-bin W: ELF file ('usr/bin/nagare') lacks PIE.
nagare-bin W: Dependency included, but may not be needed ('mpv')
```

| 告警 | 为什么不改 |
| --- | --- |
| `x86_64 应改成 $CARCH` | namcap 对 arch 专属 source 数组的通用误报。**照它改会把 aarch64 弄坏**：上游归档名里那个架构叫 `arm64`（GoReleaser 按 GOARCH 命名），不是 `$CARCH` 的 `aarch64`，两个 URL 没法用同一个变量拼出来——这正是这里要写两个 `source_*` 数组的原因 |
| `lacks FULL RELRO` / `lacks PIE` | 上游二进制的构建方式（Go，非 `-buildmode=pie`）。`-bin` 包只是重新分发官方产物，改不了也不该改 |
| `mpv 可能不需要` | namcap 只看 ELF 的动态链接表。nagare 是把 mpv 当**外部进程**拉起来的，链接表里当然没有它。这条依赖是对的 |

唯一一条真错误在写这个包时已经修掉：往 `/usr/share/icons/hicolor/` 装图标必须
`depends=('hicolor-icon-theme')`，否则 namcap 报 `E: Dependency hicolor-icon-theme
detected and not included`。

## 这个包的几个刻意选择

| 选择 | 理由 |
| --- | --- |
| `-bin` 而不是从源码构建 | 从源码要在 makepkg 环境里 `bun` 构建前端 + Go 编译，makedepends 长、两次联网解析依赖；而官方 Release 已有 minisign 签名的 `checksums.txt`，用户装到的字节 == 上游发布的字节 |
| 包名必须带 `-bin` 后缀 | 不是风格问题：AUR 提交规范原文「Packages that use prebuilt deliverables, when the sources are available, must use the `-bin` suffix」。nagare 的源码是公开的，所以这个后缀是强制的 |
| `options=('!strip')` | 归档里就是官方产物，已用 `-s -w` 构建（`file` 报 `stripped`）。再 strip 一次没收益，却会改动用户刚校验过的字节 |
| `license=('AGPL-3.0-only')` | Arch 现在用 SPDX 标识符（旧写法是 `AGPL3`）。用 `-only` 是因为仓库里没有「或任何更新版本」的授权声明，打包元数据宁可少断言 |
| desktop / 图标走 raw URL | 它们只被打进官方 deb/rpm，不在 Release 归档里。按 tag 取 = 与该版本源码严格对应，且照样受 sha256 保护 |
| 不带 `.install` 脚本 | Arch 上 `desktop-file-utils` 与 `gtk-update-icon-cache` 各自带 pacman hook，装/卸时自动刷新数据库与图标缓存。Arch 开发者手册明写「Do not call *update-desktop-database* in the .install file」「Do not call *gtk-update-icon-cache* tool in the .install file」，且**不需要**依赖这两个包 |
| 文档装进 `/usr/share/doc/nagare-bin/` | Arch 惯例按包名分目录（namcap 也按这个查），与官方 deb/rpm 的 `/usr/share/doc/nagare/` 不同是有意的 |

## 自更新的行为

pacman 装的 nagare 落在 `/usr/bin/nagare`。`internal/selfupdate` 按可执行文件路径判定
安装渠道，`/usr/` 前缀会被判成 `ChannelPackage`（见
[`internal/selfupdate/channel.go`](../../internal/selfupdate/channel.go) 的 `packagePrefixes`），
**界面只提示新版本、不执行自更新**——就地替换会与 pacman 的文件清单打架。这是既有行为，
AUR 包不需要额外配置。
