# iOS libmpv 构建

App Store 版（iPhone / iPad）的播放内核：从上游源码编出 `Libmpv.xcframework`，整套 **LGPL-2.1-or-later**
（App Store 的条款与 GPL 冲突）。CI 是 [`.github/workflows/libmpv-ios.yml`](../../../.github/workflows/libmpv-ios.yml)。

构建流程不另写一份：这里是 macOS 那套（[`scripts/macos/libmpv/`](../../macos/libmpv/README.md)）上的一层
overlay —— 同一个 `build.sh`、同一份 `versions.lock`（源码地址与 sha256）、同一套逐组件缓存与 mpv 补丁，
只换掉与目标平台有关的函数（`lib/env.sh`）和几个组件的配方（`lib/recipes.sh`）。

## 产物

一个 tar.gz，解开是 `libmpv-ios/`：

| 路径 | 内容 |
| --- | --- |
| `Libmpv.xcframework/ios-arm64/` | 真机（arm64） |
| `Libmpv.xcframework/ios-arm64_x86_64-simulator/` | 模拟器：Apple Silicon（arm64）+ Intel Mac（x86_64）合成一份（xcframework 里同一平台只能有一份） |
| `include/mpv/` | 四个头文件，给 Go 的 cgo（`-I` 指到这里，`#include <mpv/client.h>` 与 macOS 一样） |
| `licenses/` | 各组件的许可证原文（只有 LGPL 与更宽松的） |
| `info/<目标>/` | FFmpeg 的配置参数 / 结论 / 组件清单、mpv 的特性与实际编译的源文件、LuaJIT 的 JIT 开关、链接日志、工具链 |
| `manifest.json` / `MANIFEST.txt` | 组件版本、选用的许可证、源码 sha256、每个切片的平台与 minos、每个文件的 sha256 |
| `VERIFY.txt` | 这一版的 verify.sh 输出 |

每个切片是一个**动态** framework：依赖全部静态并进去，只导出 `mpv_*`，装载名 `@rpath/Libmpv.framework/Libmpv`，
只依赖系统库与系统 framework，`MinimumOSVersion` / minos 都是 **15.0**（App 的部署目标不能比它低）。
App 里用法：Xcode 里把 xcframework 加进 target 并选「Embed & Sign」；Swift `import Libmpv`，C / Objective-C
`#import <Libmpv/mpv/client.h>`。

为什么是动态的：LGPL 要让用户能换掉这个库再链接 —— 换掉 App 里的 `Libmpv.framework` 即可；App Store 也不收
散落的 dylib，所以是一个 framework 而不是二十几个库。

第二个 tar.gz 是完整对应源码（[`source-bundle.sh`](source-bundle.sh)）：这次用到的全部上游源码归档 + 两层构建脚本
+ CI 工作流 + 重新构建的步骤。随二进制一起提供，满足 LGPL 的对应源码义务。

## 与 macOS 版的区别

| | macOS | iOS |
| --- | --- | --- |
| mpv | `-Dgpl=true`（GPL-2.0+）、命令行 mpv + libmpv、cocoa-cb | **`-Dgpl=false`**、只有 libmpv；render API 走 **OpenGL ES**（iOS 上只有 OpenGL），硬解 VideoToolbox 经 `ios-gl` 互操作，声音 AudioUnit；不要 `cplugins`（App 不加载外部代码） |
| FFmpeg | `--enable-version3`（mbedTLS 要求）→ LGPL-3.0+ | **不加 `--enable-version3`** → LGPL-2.1+；协议只有 `file http tcp data`（只播本机文件与回环地址上的 `/stream`），没有 https / tls / crypto；不要 VideoToolbox 的三个滤镜（要 Metal 编译器 / iOS 16） |
| mbedTLS | 有 | **没有** |
| LuaJIT | JIT 开着 | `TARGET_SYS=iOS` + `LUAJIT_DISABLE_JIT`：iOS 不许运行时生成代码，解释器照常跑 Lua |
| freetype | 随包带 FTL 与 GPLv2 原文 | 双许可证选 FTL，只带 FTL |
| 库的形态 | 23 个 dylib + 命令行 mpv | 全部静态，链成一个 framework |

解码 / 分离 / 解析器清单与 macOS 版相同：本地文件在 iOS 上要能播桌面上能播的一切（媒体库扫描器认的
mkv mp4 avi webm mov m4v flv wmv ts rmvb 都在内）。verify.sh 核对这三张表是 macOS 版的超集，以后 macOS
加了格式而 iOS 没跟上，iOS 的验证会失败。

## 许可证

每个组件按哪个许可证分发写在 [`license-choices.txt`](license-choices.txt)，verify.sh 逐项核对：

- **FFmpeg**：configure 的结论必须是 `License: LGPL version 2.1 or later`，`CONFIG_GPL` / `CONFIG_VERSION3` /
  `CONFIG_NONFREE` 都是 0，配置参数里没有 GPL / v3 / TLS 开关。
- **mpv**：`HAVE_GPL 0`；mpv 在 `Copyright` 里列出的「GPL only」文件一个都不能出现在实际编译的源文件里
  （verify.sh 每次从 `Copyright` 现读这张清单，清单读不出来也算失败）。
- **其余组件**：选用的许可证不能是 GPL；依据为 `lock` 的，必须是 `versions.lock` 那一列或它的「OR」选项之一；
  用到却没写、写了却没用到，都算失败。随包的许可证原文里不能有 GPL 的。

## 本地构建

需要 Xcode 26（CI 钉 26.3：App 要用 iOS 26 SDK 才能上传，库用同一代工具链）以及
`meson ninja cmake nasm pkg-config autoconf automake libtool`。

```sh
for t in ios-arm64 iossim-arm64 iossim-x86_64; do
  scripts/ios/libmpv/build.sh --arch $t                 # 产物在 dist/libmpv-ios/prefix/<目标>
  scripts/ios/libmpv/package.sh framework --target $t   # → dist/libmpv-ios/fw/<目标>
done
scripts/ios/libmpv/package.sh xcframework out
scripts/ios/libmpv/verify.sh out --simulator            # 不加 --simulator 只做静态检查
scripts/ios/libmpv/manifest.sh out
```

`LIBMPV_WORK` 改工作目录（默认 `dist/libmpv-ios`），`LIBMPV_SOURCES` 改源码目录（可与 macOS 共用一份）。
`build.sh --keys --arch <目标>` 打印每个组件的缓存键。升级版本照 macOS 的 README「升级版本」改 `versions.lock`，
两条流水线都会重编；iOS 只多看一眼 `lib/recipes.sh` 里重写的五个配方要不要跟着改。

## 验证

[`verify.sh`](verify.sh)：

- 静态：三个切片齐全（真机 arm64、模拟器 arm64 + x86_64），平台（`LC_BUILD_VERSION`）对，minos ≤ 15.0，
  是动态库、装载名对、没有 `LC_RPATH`、只依赖系统库、只导出 `mpv_*`（宿主要用的 API 一个不少），
  framework 的 Info.plist / 头文件 / module map / 签名；上面「许可证」一节的全部检查；没有 TLS；
  VideoToolbox 硬解的组件都在；LuaJIT 的 `LJ_HASJIT` 是 0。另外列出每个切片引用的
  「需要声明理由的 API」（`stat` 一类、`mach_absolute_time`）—— App 的 `PrivacyInfo.xcprivacy` 要覆盖它们。
- `--simulator`：新建一台 iOS 模拟器，用本机架构对应的模拟器切片编 [`tests/sim_test.m`](tests/sim_test.m)，经
  `xcrun simctl spawn` 在模拟器里跑：初始化与版本、扫描器认的每个扩展名各解一段（视频与音频都要解出东西）、
  回环地址上的 http 能播而 https 被拒、Lua（LuaJIT，`jit.status()` 为 false，`jit.on()` 被拒）、
  render API 在 EAGLContext（OpenGL ES）上画 30 帧并读回像素（软解必过；VideoToolbox 为可选项）。

真机切片在 CI 上只能做静态检查（runner 跑不了 iOS 真机程序）：真机上的验证要等 App 接上之后在 iPhone / iPad 上过。
