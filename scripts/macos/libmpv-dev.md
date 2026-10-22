# 桌面窗口的开发期 libmpv

> **仅供本机开发，绝不能随发布出去。**
> 这里下载的是 IINA 的预编译依赖（`iina/deps-buildscripts` 的 `iina-deps-fat.zip`）。那个构建脚本仓库
> 没有许可证文件，而 GPL 要求随二进制提供对应的构建脚本 —— 用它的二进制发版做不到合规。
> 发布版用我们自己构建的 libmpv（分支 `ci/libmpv-macos` 上的流水线）。这些 dylib 不进 `dist/`、
> 不进 `.app`、不进任何 Release。

macOS 的桌面窗口（`internal/desktop`）在进程内跑 libmpv，只有带 `libmpv` 构建标签的 cgo 构建才编进来；
默认构建、CI、goreleaser 都不带它，行为与以前一样（浏览器 + 独立的 mpv 窗口）。

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
`NAGARE_DESKTOP_TEST_ADDR=127.0.0.1:<端口>` 就在那里提供截窗口（`CGWindowListCreateImage` 截本进程的
窗口，不要屏幕录制权限）、合成鼠标键盘、读窗口状态。正式构建没有这些文件。

```sh
scripts/macos/build-desktop-dev.sh ./nagare-desktop-test -tags "libmpv desktoptest"
```
