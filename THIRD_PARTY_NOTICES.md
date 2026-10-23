# 第三方组件声明

nagare 本体以 [AGPL-3.0](LICENSE) 发布。本文件列出随 nagare 分发包一起提供、或被链接进
nagare 二进制的第三方组件及其许可证。许可证以各上游源码树中的 LICENSE / COPYING 文件为准。

## macOS 包内置的 libmpv 与命令行 mpv

`nagare-<版本>_MacOS_universal.dmg` 与自更新用的 `nagare-<版本>_MacOS_universal.app.zip` 里的
`Nagare.app` 在自己的窗口里播放视频：**nagare 进程内链接 libmpv**（`Contents/Frameworks/` 下 23 个动态库，
render API 画进 nagare 的窗口），另带一个由同一批源码编出的命令行 mpv（`Contents/MacOS/mpv`，给进度条缩略图
与 `--no-window` 浏览器模式用）。macOS 的裸二进制 `nagare-<版本>_MacOS_universal.tar.gz` 与 Linux 包不捆绑
（使用系统安装的 mpv）。

这套库是 nagare 自己从上游源码构建的（[`scripts/macos/libmpv/`](scripts/macos/libmpv/README.md)，
CI 是 [`.github/workflows/libmpv-macos.yml`](.github/workflows/libmpv-macos.yml)），不是第三方预编译包。
每个源码归档的地址与 sha256 钉在 [`versions.lock`](scripts/macos/libmpv/versions.lock) 里；
包内 `Contents/Resources/libmpv/` 有这一版的构建清单（`MANIFEST.txt` / `manifest.json`：组件版本、源码哈希、
补丁、工具链、每个文件的 sha256）与各组件的许可证原文（`licenses/`）。

**整体许可证：GPL-3.0-or-later，与 nagare 一起构成一个合并作品。** 只有 mpv 本身带来 GPL
（GPL-2.0-or-later，默认的 `-Dgpl=true`）；FFmpeg 没开任何 GPL 组件，因链接 mbedTLS 用
`--enable-version3`，是 LGPL-3.0-or-later；其余依赖都是 LGPL / MIT / BSD / ISC / Apache 一类。合起来按
GPL-3.0-or-later 分发。与 Windows 包不同，这里是**同一进程内的动态链接**，nagare（AGPL-3.0）与这套库组成
合并作品；AGPL-3.0 与 GPL-3.0 第 13 条明确允许两者合并分发，合并作品里 nagare 的部分仍按 AGPL-3.0，
libmpv 的部分仍按 GPL-3.0-or-later。

| 组件 | 版本 | 上游 | 许可证 |
| --- | --- | --- | --- |
| mpv（libmpv 与命令行 mpv） | 0.41.0 + 3 个上游已合并的修复（[`patches/mpv/`](scripts/macos/libmpv/patches/mpv)） | https://github.com/mpv-player/mpv | GPL-2.0-or-later（多数源文件为 LGPL-2.1-or-later） |
| FFmpeg | 8.0.3 | https://ffmpeg.org | LGPL-3.0-or-later（`--enable-version3`，无 GPL 组件） |
| Mbed TLS | 3.6.7 | https://github.com/Mbed-TLS/mbedtls | Apache-2.0 OR GPL-2.0-or-later |
| libplacebo | 7.360.1 | https://code.videolan.org/videolan/libplacebo | LGPL-2.1-or-later |
| └ glad（生成的 OpenGL 加载器） | 73db193 | https://github.com/Dav1dde/glad | MIT |
| └ fast_float（头文件） | 97b54ca | https://github.com/fastfloat/fast_float | Apache-2.0 OR MIT OR BSL-1.0 |
| └ Vulkan-Headers（仅构建期头文件） | 450bd22 | https://github.com/KhronosGroup/Vulkan-Headers | Apache-2.0 OR MIT |
| └ Jinja / MarkupSafe（仅构建期的代码生成，不进二进制） | 1520688 / 297fc8e | https://github.com/pallets | BSD-3-Clause |
| libass | 0.17.5 | https://github.com/libass/libass | ISC |
| FreeType | 2.14.3 | https://freetype.org | FTL OR GPL-2.0-or-later |
| HarfBuzz | 14.6.0 | https://github.com/harfbuzz/harfbuzz | MIT（Modern Variant） |
| FriBidi | 1.0.17 | https://github.com/fribidi/fribidi | LGPL-2.1-or-later |
| dav1d | 1.5.4 | https://code.videolan.org/videolan/dav1d | BSD-2-Clause |
| zimg | 3.0.6 | https://github.com/sekrit-twc/zimg | WTFPL |
| Little-CMS（lcms2） | 2.19.1 | https://github.com/mm2/Little-CMS | MIT |
| libjpeg-turbo | 3.1.4.1 | https://github.com/libjpeg-turbo/libjpeg-turbo | IJG AND BSD-3-Clause AND Zlib |
| uchardet | 0.0.8 | https://gitlab.freedesktop.org/uchardet/uchardet | MPL-1.1 OR GPL-2.0-or-later OR LGPL-2.1-or-later |
| LuaJIT | 2.1（a4c0645） | https://github.com/LuaJIT/LuaJIT | MIT |

zlib、bzip2、iconv 用的是 macOS 系统自带的，不随包分发。组件或版本变了时以包内 `MANIFEST.txt` 为准。

### 完整对应源码

- **每个 GitHub Release 附带 `libmpv-macos-source-<构建号>.tar.gz`**（`<构建号>` = mpv 版本 + 构建提交，
  与包内 `MANIFEST.txt` 的「源码提交」一致）：`versions.lock` 列出的全部上游源码归档（逐个校验过 sha256）、
  我们的构建脚本、补丁、验证脚本与 CI 工作流，以及重新构建的步骤说明。发布流水线会检查它与二进制同时出现在
  `checksums.txt` 里（`scripts/release/check-gpl-sources.sh`）。
- **合并作品的其余部分**（nagare 本体与把它组装成 `Nagare.app` 的脚本：`.goreleaser.yaml`、
  `scripts/macos/build-app.sh`）就是这个仓库在该版本 tag 上的源码，Release 页自带源码归档。
- 如需任一组件的源码副本而上述途径已不可用，请在本仓库开 issue，我们会提供。

## Windows 包内置的 mpv

`nagare-<版本>_Windows_x86_64-setup.exe` 与 `nagare-<版本>_Windows_x86_64.zip` 在 `mpv\` 子目录里
捆绑了一份 mpv；Linux 包与 macOS 的裸二进制 tar.gz 不捆绑（使用系统安装的 mpv），macOS 的 `Nagare.app`
见上一节。

| | |
| --- | --- |
| 来源 | [shinchiro/mpv-winbuild-cmake](https://github.com/shinchiro/mpv-winbuild-cmake) release **20261021**，资产 `mpv-x86_64-20261021-git-e471496c56.7z` |
| mpv 修订 | git `e471496c56` |
| 构建脚本修订 | `1f8d2e76255abc48f0456d9be99b4cab9c6566e6` |
| 钉死记录 | [`scripts/windows/mpv.lock`](scripts/windows/mpv.lock)（含 sha256） |

**整体许可证：GPLv3。** mpv 本身为 GPLv2 或更新版本；该构建的 FFmpeg 以
`--enable-gpl --enable-version3` 编译并链接了 x264 / x265（GPLv2+），按 FFmpeg 的许可规则整体
即为 GPLv3。这份 mpv 是一个独立进程：nagare 通过命令行拉起它、经 JSON IPC（命名管道）通信，
不与之静态或动态链接。nagare 自身是 AGPL-3.0，与 GPLv3 的组合分发兼容。

我们对捆绑内容只做了一件事：删掉与 nagare 无关的文件关联安装器（`installer\`）和自更新脚本
（`updater.bat`、`mpv-register.bat`、`mpv-unregister.bat`）。没有重新编译、没有改动任何二进制。

### 完整对应源码

- 构建脚本快照：每个 GitHub Release 附带 `mpv-winbuild-cmake-<commit>.tar.gz`，即上表所列
  修订版的 shinchiro 仓库源码，`packages/*.cmake` 里声明了每个组件的上游 git 地址与构建参数。
- mpv 源码：<https://github.com/mpv-player/mpv>，修订 `e471496c56`。
- 其他组件：shinchiro 的构建在构建时拉取各上游仓库的当时 HEAD，精确修订版记录在该 release 的
  构建日志中（<https://github.com/shinchiro/mpv-winbuild-cmake/actions/runs/39518569492>）。
  如需任一组件的源码副本而上述途径已不可用，请在本仓库开 issue，我们会提供。

### 组件一览

以下按上表构建脚本修订版的 `packages/` 目录核对，列出主要组件（上游地址取自各 `.cmake` 文件）。

| 组件 | 上游 | 许可证 |
| --- | --- | --- |
| mpv | https://github.com/mpv-player/mpv | GPLv2+（本构建启用 GPL 特性；多数源文件为 LGPLv2.1+） |
| FFmpeg | https://github.com/FFmpeg/FFmpeg | LGPLv2.1+；本构建 `--enable-gpl --enable-version3` → GPLv3 |
| x264 | https://code.videolan.org/videolan/x264 | GPLv2+ |
| x265 | https://github.com/Multicorewareinc/x265 | GPLv2+ |
| libass | https://github.com/libass/libass | ISC |
| libplacebo | https://github.com/haasn/libplacebo | LGPLv2.1+ |
| dav1d | https://code.videolan.org/videolan/dav1d | BSD-2-Clause |
| fribidi | https://github.com/fribidi/fribidi | LGPLv2.1+ |
| HarfBuzz | https://github.com/harfbuzz/harfbuzz | MIT（Old MIT） |
| FreeType | https://github.com/freetype/freetype | FTL / GPLv2 双许可 |
| LuaJIT（openresty/luajit2） | https://github.com/openresty/luajit2 | MIT |
| libjxl | https://github.com/libjxl/libjxl | BSD-3-Clause |
| libaom | https://aomedia.googlesource.com/aom | BSD-2-Clause + AOM 专利授权 |
| SVT-AV1 | https://gitlab.com/AOMediaCodec/SVT-AV1 | BSD-3-Clause-Clear + AOM 专利授权 |
| libvpx | https://chromium.googlesource.com/webm/libvpx | BSD-3-Clause |
| opus / ogg / vorbis / libFLAC / speex | https://github.com/xiph | BSD-3-Clause |
| LAME | https://gitlab.com/shinchiro/lame | LGPLv2+ |
| OpenSSL | https://github.com/openssl/openssl | Apache-2.0 |
| Mbed TLS | https://github.com/Mbed-TLS/mbedtls | Apache-2.0 / GPLv2+ 双许可 |
| Vulkan-Loader / shaderc / SPIRV-Cross / SPIRV-Tools / glslang | https://github.com/KhronosGroup、https://github.com/google/shaderc | Apache-2.0（glslang 含 BSD-3 / MIT 部分） |
| libbluray | https://code.videolan.org/videolan/libbluray | LGPLv2.1+ |
| libdvdcss / libdvdnav / libdvdread | https://code.videolan.org/videolan | GPLv2+ |
| libjpeg-turbo | https://github.com/libjpeg-turbo/libjpeg-turbo | IJG / BSD-3-Clause / zlib |
| libpng | https://github.com/glennrp/libpng | PNG Reference Library License v2 |
| libwebp | https://chromium.googlesource.com/webm/libwebp | BSD-3-Clause |
| zlib-ng | https://github.com/zlib-ng/zlib-ng | zlib |
| xz（liblzma） | https://github.com/tukaani-project/xz | 0BSD |
| zstd | https://github.com/facebook/zstd | BSD-3-Clause / GPLv2 双许可 |
| SRT | https://github.com/Haivision/srt | MPL-2.0 |
| libssh | https://gitlab.com/libssh/libssh-mirror | LGPLv2.1 |
| curl | https://github.com/curl/curl | curl 许可证（MIT 类） |
| Little-CMS | https://github.com/mm2/Little-CMS | MIT |
| soxr | https://gitlab.com/shinchiro/soxr | LGPLv2.1+ |
| libsamplerate | https://github.com/libsndfile/libsamplerate | BSD-2-Clause |
| Rubber Band | https://github.com/breakfastquay/rubberband | GPLv2+ |
| uchardet | https://gitlab.freedesktop.org/uchardet/uchardet | MPL-1.1 / GPLv2+ / LGPLv2.1+ |
| libarchive | https://github.com/libarchive/libarchive | BSD-2-Clause |
| MuJS | https://codeberg.org/ccxvii/mujs | ISC |
| libsixel | https://github.com/saitoha/libsixel | MIT |
| fontconfig | https://gitlab.freedesktop.org/fontconfig/fontconfig | MIT 类（HPND） |
| GNU libiconv | https://ftp.gnu.org/pub/gnu/libiconv | LGPLv2+ |
| libxml2 | https://github.com/GNOME/libxml2 | MIT |
| zimg | https://github.com/sekrit-twc/zimg | WTFPL |
| VapourSynth（头文件） | https://github.com/vapoursynth/vapoursynth | LGPLv2.1+ |
| libaribcaption | https://github.com/xqq/libaribcaption | MIT |
| libunibreak | https://github.com/adah1972/libunibreak | zlib |
| subrandr | https://github.com/afishhh/subrandr | MPL-2.0 |
| OpenAL Soft | https://github.com/kcat/openal-soft | LGPLv2+ |
| libbs2b | https://github.com/alexmarsev/libbs2b | MIT |
| libmysofa | https://github.com/hoene/libmysofa | BSD-3-Clause |
| libmodplug | https://github.com/Konstanty/libmodplug | Public Domain |
| libopenmpt | https://lib.openmpt.org | BSD-3-Clause |
| Game_Music_Emu | https://bitbucket.org/mpyne/game-music-emu | LGPLv2.1+ |
| Nettle / GMP | https://gitlab.com/shinchiro/nettle、https://ftp.gnu.org/gnu/gmp | LGPLv3+ / GPLv2+ 双许可 |
| d3dcompiler_43.dll | Microsoft DirectX 可再分发组件 | 专有（随 mpv 官方 Windows 包一并再分发） |

## Go 依赖

随 nagare 二进制一起链接（`go.mod` 的 require 列表；`go list -m all` 可查全部）：

| 模块 | 许可证 |
| --- | --- |
| github.com/BurntSushi/toml | MIT |
| golang.org/x/text | BSD-3-Clause |
| golang.org/x/sys | BSD-3-Clause |
| golang.org/x/mod | BSD-3-Clause |
| gopkg.in/yaml.v3 / go.yaml.in/yaml/v3 | MIT（部分文件 Apache-2.0） |
| fyne.io/systray（托盘图标） | BSD-3-Clause |
| github.com/godbus/dbus/v5（systray 的 Linux 后端，Linux 桌面通知） | BSD-2-Clause |
| github.com/Microsoft/go-winio（Windows 上连接 mpv 的命名管道） | MIT |

仅用于测试、不进入二进制：github.com/stretchr/testify（MIT）。

## 内置的 Anime4K 着色器

设置页「画质增强」用的 GLSL 着色器内嵌在 nagare 二进制里（[`internal/mpv/anime4k/`](internal/mpv/anime4k/)），
播放时写进运行时目录交给 mpv 加载。原样复制、未作改动：

| | |
| --- | --- |
| 来源 | [bloc97/Anime4K](https://github.com/bloc97/Anime4K) 修订 `7684e9586f8dcc738af08a1cdceb024cc184f426`（`glsl/Restore`、`glsl/Upscale`） |
| 文件 | `Anime4K_Clamp_Highlights` · `Anime4K_Restore_CNN_M` · `Anime4K_Restore_CNN_VL` · `Anime4K_Upscale_CNN_x2_S` · `Anime4K_Upscale_CNN_x2_M` · `Anime4K_Upscale_CNN_x2_VL`（MIT，版权归 bloc97，许可证见 [`internal/mpv/anime4k/LICENSE`](internal/mpv/anime4k/LICENSE) 与各文件头）；`Anime4K_AutoDownscalePre_x2` · `Anime4K_AutoDownscalePre_x4`（Unlicense，公有领域，见文件头） |

## 播放器界面里的 thumbfast

进度条缩略图由 thumbfast 生成：它随 nagare 的播放器界面一起内嵌在二进制里
（[`internal/mpv/ui/scripts/thumbfast.lua`](internal/mpv/ui/scripts/thumbfast.lua)），起播时写进运行时目录交给
mpv 加载。原样复制、未作改动；显示缩略图的界面代码是 nagare 自己的。

| | |
| --- | --- |
| 来源 | [po5/thumbfast](https://github.com/po5/thumbfast) 修订 `0f711de3138c9bd6718209d819ac54022c23ded2` |
| 许可证 | [Mozilla Public License 2.0](https://mozilla.org/MPL/2.0/)（文件级著佐权：分发的就是源码文件本身，许可证声明在文件头） |
| 设置 | [`internal/mpv/ui/script-opts/thumbfast.conf`](internal/mpv/ui/script-opts/thumbfast.conf)（nagare 写的，不属于 thumbfast） |

## 前端依赖

内嵌进二进制的浏览器界面（`frontend/package.json` 的 `dependencies`）：

| 包 | 许可证 |
| --- | --- |
| react / react-dom | MIT |
| @tanstack/react-router | MIT |
| motion | MIT |
| embla-carousel / embla-carousel-react / embla-carousel-autoplay / embla-carousel-reactive-utils (8.6.0) | MIT |

构建期依赖（Vite、TypeScript、Vitest 等）不进入分发包。

### 界面参考

导航、发现页、媒体库与设置页参照 [5rahim/seanime](https://github.com/5rahim/seanime)
v3.10.2（修订 `9bdd052`）的 `seanime-web` 组件布局、尺寸和配色，在 nagare 的 React / Vite
结构中实现。上游使用 GPL-3.0，许可证见其源码树的 LICENSE。

### Inter 字体

随界面提供 Inter Variable 的 Latin WOFF2，来自 `@fontsource-variable/inter@5.2.8`。
版权归 Inter Project Authors 所有，使用 SIL Open Font License 1.1；完整许可证随文件提供于
[`frontend/public/fonts/OFL.txt`](frontend/public/fonts/OFL.txt)。字体由本机提供，无第三方字体请求。

### 演示图片

发现、列表与放送页的静态演示封面及横幅来自 AniList 公开媒体元数据中的图片地址。
图片权利归各自权利人所有，不适用 nagare 的代码许可证；逐项来源见
[`frontend/public/demo-art/SOURCES.md`](frontend/public/demo-art/SOURCES.md)。
