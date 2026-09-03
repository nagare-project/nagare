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
| Linux deb / rpm、Homebrew | ❌ 由包管理器管，界面提示用 `apt` / `dnf` / `brew` 升级 |

不支持时界面会说明原因并给下载页链接，不会只是按钮点不动。

### 备用分发源

Release 资产被投诉下架是同类开源项目真实发生过的事。客户端的下载地址前缀可以用
环境变量覆盖：

```bash
NAGARE_UPDATE_BASE_URL=https://example.invalid/nagare/releases/download
```

必须是 `https://`。**更新检查或下载失败绝不影响已安装版本的运行**——这是设计约束，
不是尽力而为。
