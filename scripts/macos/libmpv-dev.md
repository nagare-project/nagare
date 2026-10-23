# 桌面窗口的开发期 libmpv

> **仅供本机开发，绝不能随发布出去。**
> 这里下载的是 IINA 的预编译依赖（`iina/deps-buildscripts` 的 `iina-deps-fat.zip`）。那个构建脚本仓库
> 没有许可证文件，而 GPL 要求随二进制提供对应的构建脚本 —— 用它的二进制发版做不到合规。
> 发布版用我们自己构建的 libmpv（[`scripts/macos/libmpv/`](libmpv/README.md)，发布流水线在同一次运行里现编、现验，
> 见 `docs/releasing.md`「macOS：Nagare.app 里的 libmpv」）。这些 dylib 不进 `dist/`、不进 `.app`、不进任何 Release
> —— `build-app.sh` 只收按 `manifest.json` 核对得上的自建产物，这里的目录没有清单，拿去打包会直接失败。

macOS 的桌面窗口（`internal/desktop`）在进程内跑 libmpv，只有带 `libmpv` 构建标签的 cgo 构建才编进来；
默认构建与 CI 的其余 job 都不带它，行为与以前一样（浏览器 + 独立的 mpv 窗口）。发布的 `Nagare.app` 带它
（goreleaser 的 `darwin-desktop` 构建），macOS 的裸二进制 tar.gz 不带。

想在本机用自建的那套而不是 IINA 的：取一份 `libmpv-macos.yml` 的产物（`gh run download <运行号> -n libmpv-macos-universal`），
解开后把 `CGO_CFLAGS=-I<目录>/include`、`CGO_LDFLAGS=-L<目录>/Frameworks` 换进下面的构建命令，再加
`-ldflags=-extldflags=-Wl,-rpath,<目录>/Frameworks`。进度条缩略图要一个命令行 mpv：自建产物里的 `MacOS/mpv`，
或者 Homebrew 的 mpv（探测到哪个用哪个）。

## 用法

```sh
scripts/macos/fetch-libmpv-dev.sh        # 下载、校验 sha256、解压到 .libmpv-dev/（已 gitignore）
(cd frontend && bun run build)           # 想在窗口里看到界面就先编前端
scripts/macos/build-desktop-dev.sh       # 输出 ./nagare-desktop-dev，运行时从 .libmpv-dev/lib 加载 libmpv
```

- 下载的两样都钉了 sha256：`iina-deps-fat.zip`（iina-1.5.0-release，universal、minos 11、mpv 0.41.0）与
  mpv v0.41.0 的四个公开头文件。归档里只能是顶层的 `lib*.dylib`，别的条目、符号链接一律拒绝解压。
- 本机的 CommandLineTools SDK 太旧（11.0），`net/http` 引用的 `SecTrustCopyCertificateChain` 不在它的
  tbd 里：构建脚本带 `-extldflags=-Wl,-U,_SecTrustCopyCertificateChain`，让它留到运行时解析（macOS 12 起都有）。
- 运行：与平常一样（要托盘，`--no-tray` 时没有 Cocoa 事件循环，退回浏览器模式）；`--no-window` 也退回浏览器模式。
  排查 mpv 时设 `NAGARE_DESKTOP_MPV_LOG=<文件>`（日志里会有媒体地址，只在显式设置时写）。

## 真机测试构建

`-tags "libmpv desktoptest"` 多编一个遥控（`internal/desktop/testctl_darwin.*`）：设了
`NAGARE_DESKTOP_TEST_ADDR=127.0.0.1:<端口>` 与 `NAGARE_DESKTOP_TEST_SECRET_FILE=<文件>` 就在那里提供截窗口
（`CGWindowListCreateImage` 截本进程的窗口，不要屏幕录制权限）、合成鼠标键盘、读窗口状态。遥控只认 Host 正好是
这个地址、并且带着 `X-Nagare-Test-Secret` 请求头的请求；密钥每次运行随机生成，写进那个文件（0600），脚本从文件里读，
别打印出来。正式构建没有这些文件。

```sh
scripts/macos/build-desktop-dev.sh ./nagare-desktop-test -tags "libmpv desktoptest"
```

CI 的冒烟测试（`desktop-mac` job）用的是自建 libmpv，本机也能照跑：先把 `libmpv-macos.yml` 的两个 artifact 用
`scripts/macos/libmpv/unpack.sh` 解开，再

```sh
SMOKE_EXTLDFLAGS=-Wl,-U,_SecTrustCopyCertificateChain scripts/macos/build-desktop-smoke.sh <libmpv 目录> /tmp/smoke-app
python3 -I -B scripts/macos/desktop-smoke.py --app /tmp/smoke-app --media internal/desktop/testdata/smoke-40s.mp4 --work <空目录>
```

`NAGARE_DESKTOP_TEST_SOFTWARE_GL=force` 让测试构建直接用软件渲染器（复现没有 GPU 的 runner），`=1` 是没有硬件
OpenGL 时才退过去（CI 用这个）。冒烟会弹出窗口、起播、⌘Q 退出，跑的时候别动鼠标。
