# macOS libmpv 构建

nagare 在 macOS 上把 mpv 嵌进自己的窗口（libmpv 的 render API，OpenGL，同进程）。这里是那套
libmpv 的构建脚本：全部从上游源码编，每份源码都在 [`versions.lock`](versions.lock) 里钉死地址与
sha256，CI 是 [`.github/workflows/libmpv-macos.yml`](../../../.github/workflows/libmpv-macos.yml)。

不用现成的二进制：mpv 官方包没有 libmpv、minos 是 14/15；Homebrew 只有 arm64、minos ≥ 15；
media-kit 没有 Lua；IINA 的 deps 构建脚本仓库没有许可证，我们没法把它当 GPL 的对应源码随包分发。
这里的脚本是自己写的（只参考了公开的事实：版本、configure 选项），不含 IINA 的文件。

## 产物

一个 tar.gz，解开是 `libmpv-macos-universal/`，布局对应 `Nagare.app/Contents`：

| 路径 | 内容 |
| --- | --- |
| `Frameworks/` | `libmpv.2.dylib` 与它的全部非系统依赖，共 23 个（mbedTLS 自带的 `libeverest` / `libp256m` 两个小库也在内），**都是真实文件、没有符号链接**（自更新拒绝符号链接）；装载名一律 `@rpath/<文件名>`，没有 `LC_RPATH` |
| `MacOS/mpv` | 命令行 mpv（thumbfast 与浏览器模式用），链接同一批库，唯一的 `LC_RPATH` 是 `@executable_path/../Frameworks` |
| `include/mpv/` | `client.h` `render.h` `render_gl.h` `stream_cb.h` |
| `licenses/` | 各组件的许可证原文 |
| `manifest.json` / `MANIFEST.txt` | 组件版本、许可证、源码 sha256、补丁、工具链；每个文件的 sha256；每个 Mach-O 的 `lipo -info`、各架构 minos、装载名、依赖（`otool -L`）与 rpath |
| `VERIFY.txt` | 这一版的 verify.sh 输出 |
| `info/<架构>/` | FFmpeg 的 configure 参数与实际开出来的组件、mpv 的特性列表 |

全部 Mach-O 都是 universal（arm64 + x86_64）、`minos 13.0`（= `LSMinimumSystemVersion`）、ad-hoc 签名。
命令行 mpv 把 mpv 自身的代码直接编了进去（meson 的做法），所以它不依赖 `libmpv.2.dylib`，
只共享其余的库。

体积（0.41.0 这一版）：`Frameworks/` 42 MB（其中 libavcodec 13 MB、libmpv 8 MB），`MacOS/mpv` 7.7 MB，
整个 tar.gz 约 24 MB；源码包约 65 MB。CI 冷构建每个架构 6–8 分钟（两个架构并行），组件全部命中缓存时
整条流水线约 4 分钟（大头是验证）。

第二个 tar.gz 是完整对应源码（见下文「GPL 义务」）。

## 组件

| 组件 | 用途 |
| --- | --- |
| mpv 0.41.0 | `-Dlibmpv=true -Dcplayer=true`；Lua = LuaJIT；cocoa-cb（命令行 mpv 的窗口）、gl-cocoa、videotoolbox-gl；没有 Vulkan / JavaScript / 蓝光 / DVD / libarchive / rubberband / SDL |
| FFmpeg 8.0.3 | 只开番剧发布会用到的组件（见下），VideoToolbox / AudioToolbox |
| libplacebo 7.360.1 | 只 OpenGL（render API 只有 OpenGL），带 lcms2 |
| libass 0.17.5 + FreeType + HarfBuzz + FriBidi | 字幕；字体走 CoreText，不带 fontconfig |
| LuaJIT 2.1 | nagare 的播放器界面是 Lua |
| dav1d | AV1 软解 |
| zimg | mpv 的截图 / 软件缩放，以及 thumbfast 给 HDR 片源做缩略图的 zscale + tonemap |
| lcms2、uchardet、libjpeg-turbo | 色彩管理；老字幕的编码探测；jpg 截图（mpv 默认格式） |
| mbedTLS 3.6 LTS | https（见「TLS」） |
| zlib / bzip2 / iconv | 用 macOS 自带的 |

上游的三个 mpv 修复以补丁形式打上（[`patches/mpv/`](patches/mpv)，都是 mpv 仓库里已合并的提交，
文件头写着原提交号）：lcms2 ≥ 2.18 下 BT.1886 曲线出 NaN 导致 `--icc-profile-auto` 黑屏、
CoreAudio 初始化失败时的清理（两条）。

**FFmpeg 组件**（[`lib/recipes.sh`](lib/recipes.sh) 的 `recipe_ffmpeg`）：
解码 h264 / hevc（含 10bit）/ av1（dav1d，外加给 VideoToolbox 用的原生 av1）/ vp8 / vp9 / mpeg1/2/4 /
msmpeg4 / vc1 / wmv1-3 / rv10-40 / flv（Sorenson）/ vp6 / mjpeg / png / webp，aac / ac3 / eac3 / truehd / dts / flac /
opus / vorbis / mp1-3 / alac / wma（v1 / v2 / pro / voice / lossless）/ RealAudio（14.4 / 28.8 / cook / sipr / atrac3 /
ralf）/ adpcm（swf / ms / ima_wav）/ nellymoser / pcm，ass / srt / webvtt / mov_text / pgs / dvdsub / dvbsub / cc；
分离器 matroska（含 webm）/ mp4 / mpegts / mpegps / avi / flv / asf（wmv）/ rm（rmvb）/ m4v / ogg / hls 与各种裸流和
字幕文件；协议 file / http / https / tls / crypto；
编码器只有截图与 thumbfast 用的 mjpeg / png / rawvideo（+ image2 封装）；滤镜是 mpv 与 thumbfast
用到的那几个（裁剪、缩放、补边、翻转、zscale、tonemap、去隔行、音量整形）。

**媒体库扫描器认的每个扩展名都要播得了**（`internal/library/scan.go` 的 `scanVideoExts`：mkv mp4 avi webm mov m4v
flv wmv ts rmvb）：Nagare.app 里的 mpv 是探测时排第一的，删掉一个分离器或解码器，用户以前用 Homebrew mpv 能放的
文件就会放不了。verify.sh 对每个扩展名各解一段测试片段，并核对清单里这些格式要用的组件都在（ffmpeg 编不出
RV30/40、cook、WMV 9、VP6 这些，只能查组件）；`scanVideoExts` 加了扩展名而没有对应的测试片段，verify.sh 直接失败。

## TLS

选 **mbedTLS 3.6 LTS**：
- 小：三个库加起来每个架构约 1 MB。OpenSSL 3 是 6 MB 左右；GnuTLS 要拖上 nettle / gmp / libtasn1 /
  p11-kit / libunistring / libidn2。
- 许可证：Apache-2.0 OR GPL-2.0-or-later，可以随 GPL 构建分发。FFmpeg 的 configure 把它归为
  「需要 `--enable-version3`」，所以 FFmpeg 整体是 LGPL-3.0-or-later。
- 支持 TLS 1.3。没选系统的 SecureTransport：苹果已废弃、只到 TLS 1.2。

mbedTLS 不读系统钥匙串。mpv 默认 `tls-verify=no`（与其他平台的 mpv 一样）；要校验证书时传
`--tls-verify=yes --tls-ca-file=/etc/ssl/cert.pem` —— macOS 自带这份 CA 列表。verify.sh 用本机
自签 CA 测了「校验通过」「不信任即拒绝」「不校验」三种情况和 HLS。

## GPL 义务

**谁让它成了 GPL：** 只有 mpv 本身（默认的 `-Dgpl=true`，GPL-2.0-or-later）。FFmpeg 没开任何 GPL
组件（不加 `--enable-gpl`，是 LGPL-3.0-or-later），其余依赖都是 LGPL / MIT / BSD / ISC / Apache 一类。
mpv 的 GPL 专属代码（X11、OSS、JACK、DVD、CDDA、DVB、caca、direct3d）在 macOS 上一个都不编，
所以以后真需要 LGPL 版，把 `-Dgpl=false` 即可得到 LGPL-3.0-or-later 的一套。
整套合起来按 GPL-3.0-or-later 分发；nagare 是 AGPL-3.0，同进程链接是兼容的（GPLv3 / AGPLv3 §13）。
`THIRD_PARTY_NOTICES.md` 的「macOS 包内置的 libmpv 与命令行 mpv」一节写的就是这件事。

**随二进制要提供的对应源码：** CI 的第二个 artifact（`libmpv-macos-source-*.tar.gz`），
由 [`source-bundle.sh`](source-bundle.sh) 生成：`versions.lock` 里的全部上游源码归档（逐个校验过
sha256）+ 本目录全部脚本、补丁、测试 + CI 工作流 + 重新构建的说明。发版时它和二进制一起挂在
同一个 Release 上（`build-app.sh` 放进 `dist/mpv-sources/`，`scripts/release/check-gpl-sources.sh` 查它在不在）。

## 本地构建

需要 macOS + Xcode（CI 钉的是 16.4）以及 `meson ninja cmake nasm pkg-config autoconf automake libtool`
（`brew install meson ninja cmake nasm pkgconf autoconf automake libtool`）。两个架构可以在任意一种 Mac 上编；
在 Apple Silicon 上装了 Rosetta 时 verify.sh 会把 x86_64 切片也跑一遍。

```sh
scripts/macos/libmpv/build.sh --arch arm64          # 产物在 dist/libmpv-macos/prefix/arm64
scripts/macos/libmpv/build.sh --arch x86_64
scripts/macos/libmpv/package.sh thin --arch arm64
scripts/macos/libmpv/package.sh thin --arch x86_64
scripts/macos/libmpv/package.sh universal dist/libmpv-macos/thin/arm64 dist/libmpv-macos/thin/x86_64 out
scripts/macos/libmpv/verify.sh out
```

`LIBMPV_WORK` 改工作目录（默认 `dist/libmpv-macos`），`LIBMPV_SOURCES` 改源码目录。
`build.sh --keys` 打印每个组件的缓存键；`build.sh --arch arm64 ffmpeg` 只编 FFmpeg 和它的依赖。

## 升级版本

1. 改 `versions.lock` 那一行的版本、文件名、地址，下载算 sha256：`shasum -a 256 <文件>`。
   libplacebo 的五个子模块要跟着它新版本钉的提交走（`gh api repos/haasn/libplacebo/contents/3rdparty?ref=<tag>`）。
2. 新版本改了构建选项就改 `lib/recipes.sh` 里对应的 `recipe_*`；`patches/` 里已被上游收进去的补丁删掉。
3. 推到 `ci/libmpv-macos` 跑一遍：只有改了的组件和依赖它的组件会重编。`VERIFY.txt` 全过、`MANIFEST.txt`
   里的版本与 minos 对得上才算完。
4. mpv 升大版本时同步看一遍 nagare 的 Lua 界面（verify.sh 会在新 LuaJIT 上加载它，但只看有没有报错）。

## 测试片段

`tests/media/` 是 verify.sh 用的几秒钟的小文件（H.264 + AAC + ASS/SRT/WebVTT、HEVC 10bit + FLAC、
AV1 + Opus、VP9 + Vorbis、MPEG-2 + AC-3、MPEG-4 + MP3、mp4 的 E-AC-3 + mov_text、DTS / TrueHD / PCM 音轨、
HLS，以及按扩展名补的 mov（H.264 + ALAC）、m4v（MPEG-4 + AAC）、flv（Sorenson Spark + ADPCM）、
wmv（WMV 8 + WMA 2）、rmvb（RealVideo 2 + RealAudio 14.4））。由 [`tests/make-media.sh`](tests/make-media.sh) 用一个全功能的 ffmpeg 生成后入库。

## 验证过的环境

- CI：macos-15（Apple Silicon 虚拟机，macOS 15）上 arm64 切片跑全套，x86_64 切片在 Rosetta 下跑主要几项；
  render API（OpenGL，软解与 VideoToolbox）、cocoa-cb 窗口、VideoToolbox 硬解都通过。
- 本地：Intel Mac，macOS 14.5，真显卡，x86_64 切片原生跑全套，全部通过。
- **没验证过：macOS 13**（GitHub 已经没有 macOS 13 的 runner）。minos 是 13.0，链接的系统库与框架
  （含 /usr/lib/swift 下的 Swift 运行库）在 13 上都有，但没有真机跑过。
