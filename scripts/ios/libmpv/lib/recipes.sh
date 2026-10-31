# shellcheck shell=bash
# iOS 的组件配方：macOS 的 build.sh 经 LIBMPV_OVERLAY 引入本文件（在它自己的 env.sh / sources.sh /
# recipes.sh 之后），所以这里只写与 macOS 不同的部分，其余配方原样复用：
#   dav1d zimg freetype fribidi harfbuzz libass lcms2 libplacebo —— 原样（构建助手换成了 iOS 的静态库版本）
#   libjpeg uchardet luajit ffmpeg mpv                           —— 下面重写
#   mbedtls                                                       —— 不要（iOS 版不播网络源，不带 TLS）
#
# 许可证：App Store 版只能是 LGPL 一类。mpv -Dgpl=false；FFmpeg 不加 --enable-gpl，也不加
# --enable-version3（macOS 版是因为 mbedTLS 才要 v3），于是整套是 LGPL-2.1-or-later。
# 每个组件选用哪个许可证写在 ../license-choices.txt，verify.sh 逐项核对。

# shellcheck source=env.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/env.sh"

DEP_ORDER="dav1d zimg freetype fribidi harfbuzz libass lcms2 libplacebo libjpeg uchardet luajit ffmpeg mpv"

# 这次构建用到的全部源码名（versions.lock 的第一列）：verify.sh 拿它核对许可证，source-bundle.sh 按它打包
ios_used_sources() {
  local d s
  for d in $DEP_ORDER; do
    for s in $(eval "printf '%s ' \"\$${d}_sources\""); do echo "$s"; done
  done
}

# 双许可证（FTL OR GPL-2.0-or-later）选 FTL：不随包带 GPL 的许可证原文
freetype_licenses="LICENSE.TXT docs/FTL.TXT"

recipe_libjpeg() {
  cmake_install "$SRC" -DENABLE_STATIC=ON -DENABLE_SHARED=OFF -DWITH_TURBOJPEG=OFF \
    -DWITH_TOOLS=OFF -DWITH_TESTS=OFF -DREQUIRE_SIMD=ON
}

recipe_uchardet() {
  # BUILD_SHARED_LIBS=OFF（cmake_install 已给）时它只编一个静态库
  cmake_install "$SRC" -DBUILD_BINARY=OFF
  # uchardet.pc 写死了 -lstdc++，iOS SDK 里没有 libstdc++（只有 libc++），静态链接时会找不到
  local pc="$DEST$PREFIX/lib/pkgconfig/uchardet.pc"
  sed -i '' -e 's/-lstdc++/-lc++/g' "$pc"
  if grep -q -- '-lstdc++' "$pc"; then die "uchardet.pc 里还有 -lstdc++"; fi
}

recipe_luajit() {
  # iOS 不许运行时生成代码：TARGET_SYS=iOS 时 lj_arch.h 自己就会定义 LJ_OS_NOJIT，这里再显式关一次，
  # 并在下面读回 LJ_HASJIT 确认（verify.sh 还会在模拟器里跑 jit.status() 再确认一次）。
  # HOST_CC 编的是构建机上跑的代码生成器，目标平台只经 TARGET_FLAGS 传进去（同 macOS 版）。
  local mk=(
    PREFIX="$PREFIX"
    CC=clang HOST_CC=clang CFLAGS= LDFLAGS=
    TARGET_FLAGS="$(arch_flags "$ARCH")"
    TARGET_SYS=iOS BUILDMODE=static
    XCFLAGS=-DLUAJIT_DISABLE_JIT
  )
  make -C "$SRC" -j "$JOBS" "${mk[@]}" amalg
  make -C "$SRC" "${mk[@]}" DESTDIR="$DEST" install
  # 命令行 luajit 用不上
  rm -rf "${DEST:?}$PREFIX/bin"
  # luajit.pc 的 Libs.private 带着 -Wl,-E（GNU ld 的导出全部符号），Apple 的链接器不认，静态链接时会报错
  local pc="$DEST$PREFIX/lib/pkgconfig/luajit.pc"
  sed -i '' -e 's/-Wl,-E //' "$pc"
  if grep -q -- '-Wl,-E' "$pc"; then die "luajit.pc 里还有 -Wl,-E"; fi
  local info="$DEST$PREFIX/share/nagare-libmpv"
  mkdir -p "$info"
  # shellcheck disable=SC2046 # arch_flags 故意按空白拆开
  clang $(arch_flags "$ARCH") -DLUAJIT_DISABLE_JIT -I"$SRC/src" -dM -E -x c "$SRC/src/lj_arch.h" \
    | grep -E '^#define (LJ_HASJIT|LJ_TARGET_IOS|LJ_OS_NOJIT|LJ_HASFFI) ' | sort > "$info/luajit-jit.txt"
  grep -qx '#define LJ_HASJIT 0' "$info/luajit-jit.txt" || die "LuaJIT 的 JIT 没有关掉：$(cat "$info/luajit-jit.txt")"
}

ffmpeg_deps="dav1d zimg"
ffmpeg_licenses="LICENSE.md COPYING.LGPLv2.1"
recipe_ffmpeg() {
  # 解码 / 分离 / 解析器清单与 macOS 版（scripts/macos/libmpv/lib/recipes.sh）相同：
  # 本地文件在 iOS 上要能播桌面上能播的一切。verify.sh 会核对这三张表是 macOS 版的超集。
  # 与 macOS 版的区别：
  #   协议只有 file / http / tcp / data —— iOS 版只播本机文件与回环地址上的 /stream（NAS 由 Go 转出来），
  #     不带 https / tls / crypto，也就不要 mbedTLS 与 --enable-version3；
  #   不要 VideoToolbox 的三个滤镜（yadif_videotoolbox 要 Metal 编译器，scale_vt 要 iOS 16）。
  local FF_DECODERS="h264 hevc av1 libdav1d vp8 vp9 mpeg1video mpeg2video mpeg4 msmpeg4v1 msmpeg4v2 msmpeg4v3 h263 vc1 wmv3
    wmv1 wmv2 rv10 rv20 rv30 rv40 flv vp6 vp6f vp6a
    mjpeg png webp
    aac aac_latm ac3 eac3 truehd mlp dca flac opus vorbis mp1 mp1float mp2 mp2float mp3 mp3float alac
    wmav1 wmav2 wmapro wmavoice wmalossless ra_144 ra_288 cook sipr atrac3 ralf adpcm_swf nellymoser adpcm_ms adpcm_ima_wav
    pcm_s8 pcm_u8 pcm_s16le pcm_s16be pcm_s24le pcm_s24be pcm_s32le pcm_s32be pcm_f32le pcm_f32be pcm_f64le
    pcm_bluray pcm_dvd pcm_alaw pcm_mulaw
    aac_at ac3_at eac3_at mp3_at alac_at
    ass ssa subrip srt webvtt movtext text microdvd pgssub dvdsub dvbsub ccaption"
  local FF_DEMUXERS="matroska mov mpegts mpegps avi flv ogg hls wav flac mp3 aac ac3 eac3 dts dtshd truehd mlp h264 hevc
    asf rm m4v
    ass srt webvtt sup vobsub microdvd image2 image_jpeg_pipe image_png_pipe image_webp_pipe"
  local FF_PARSERS="h264 hevc av1 vp8 vp9 mpeg4video mpegvideo vc1 h263 mjpeg png webp aac aac_latm ac3 dca flac mlp
    mpegaudio opus vorbis dvdsub dvbsub rv34 cook sipr"
  local FF_ENCODERS="mjpeg png rawvideo"
  local FF_MUXERS="image2"
  local FF_PROTOCOLS="file http tcp data"
  local FF_FILTERS="crop pad scale format hflip vflip transpose rotate fps setpts null yadif bwdif zscale tonemap
    hwupload hwdownload
    anull aformat aresample asetpts volume pan loudnorm dynaudnorm acompressor"
  local FF_HWACCELS="h264_videotoolbox hevc_videotoolbox vp9_videotoolbox av1_videotoolbox mpeg1_videotoolbox
    mpeg2_videotoolbox mpeg4_videotoolbox h263_videotoolbox"
  local flags
  flags="$(arch_flags "$ARCH") -I$PREFIX/include"
  isolate_pkg_config
  local args=(
    --prefix="$PREFIX" --libdir="$PREFIX/lib"
    --target-os=darwin --arch="$(meson_cpu "$ARCH")" --enable-cross-compile
    --cc=clang --cxx=clang++ --objcc=clang
    --extra-cflags="$flags" --extra-cxxflags="$flags" --extra-objcflags="$flags"
    --extra-ldflags="$(arch_flags "$ARCH") -L$PREFIX/lib"
    --pkg-config=pkg-config --pkg-config-flags=--static
    --enable-static --disable-shared --enable-pic
    --disable-programs --disable-doc --disable-debug --disable-avdevice
    # 不加 --enable-gpl、--enable-version3、--enable-nonfree：LGPL-2.1-or-later（verify.sh 核对 configure 的结论）
    --enable-pthreads --enable-network --enable-zlib --enable-bzlib --enable-iconv
    --enable-videotoolbox --enable-audiotoolbox
    --enable-libdav1d --enable-libzimg
    # 自动探测里不要的：不带任何 TLS；不要设备 / 窗口 / Metal
    --disable-securetransport --disable-appkit --disable-avfoundation --disable-coreimage --disable-metal
    --disable-sdl2 --disable-xlib --disable-libxcb --disable-lzma --disable-vulkan
    --disable-everything
    --enable-decoder="$(ff_list "$FF_DECODERS")"
    --enable-demuxer="$(ff_list "$FF_DEMUXERS")"
    --enable-parser="$(ff_list "$FF_PARSERS")"
    --enable-encoder="$(ff_list "$FF_ENCODERS")"
    --enable-muxer="$(ff_list "$FF_MUXERS")"
    --enable-protocol="$(ff_list "$FF_PROTOCOLS")"
    --enable-filter="$(ff_list "$FF_FILTERS")"
    --enable-hwaccel="$(ff_list "$FF_HWACCELS")"
  )
  (
    cd "$OBJ"
    "$SRC/configure" "${args[@]}" > configure.out || { tail -80 ffbuild/config.log; exit 1; }
    cat configure.out
    make -j "$JOBS"
    make install DESTDIR="$DEST"
  )
  local info="$DEST$PREFIX/share/nagare-libmpv"
  mkdir -p "$info"
  printf '%s\n' "${args[@]}" > "$info/ffmpeg-configure-args.txt"
  cp "$OBJ/configure.out" "$info/ffmpeg-configure-summary.txt"
  grep -E '^#define CONFIG_[A-Z0-9_]+_(DECODER|DEMUXER|PARSER|ENCODER|MUXER|PROTOCOL|FILTER|HWACCEL|BSF) 1' \
    "$OBJ/config_components.h" | awk '{print $2}' | sed 's/^CONFIG_//' | tr 'A-Z' 'a-z' | sort \
    > "$info/ffmpeg-components.txt"
  grep -E '^#define (CONFIG_GPL|CONFIG_GPLV3|CONFIG_VERSION3|CONFIG_NONFREE|FFMPEG_LICENSE) ' "$OBJ/config.h" \
    > "$info/ffmpeg-license.txt"
}

mpv_licenses="LICENSE.LGPL Copyright"
recipe_mpv() {
  # 只编 libmpv（静态库，package.sh 再链成 framework）；渲染走 render API 的 OpenGL ES（iOS 上 render API
  # 只有 OpenGL），硬解是 VideoToolbox 经 ios-gl 互操作；声音走 AudioUnit（RemoteIO）。
  # 不要 videotoolbox-gl（那是 macOS 的 CGL 互操作）、coreaudio / avfoundation（会编进 macOS 才有的
  # CoreAudio HAL 调用）、cocoa / swift（macOS 的窗口）、cplugins（App 里不加载外部代码）。
  #
  # mpv 的 meson 在 darwin 上会去找 macOS SDK，并把它的 -isysroot 加进链接参数：给它 iOS 的 SDK，
  # 版本填 iOS SDK 的版本（非 0.0 才不会再去问 xcrun 要 macOS 的）。下面把 macOS 那几档特性都关了，
  # 这个版本号不影响别的。
  (
    MACOS_SDK=$(sdk_path "$ARCH")
    MACOS_SDK_VERSION=$(xcrun --sdk "$(target_sdk "$ARCH")" --show-sdk-version)
    export MACOS_SDK MACOS_SDK_VERSION
    meson_install "$SRC" \
      -Dlibmpv=true -Dcplayer=false -Dgpl=false -Dcplugins=disabled \
      -Dbuild-date=false -Dtests=false -Dfuzzers=false \
      -Dmanpage-build=disabled -Dhtml-build=disabled -Dpdf-build=disabled \
      -Dlua=luajit -Djavascript=disabled \
      -Dlibarchive=disabled -Dlibbluray=disabled -Ddvdnav=disabled -Dcdda=disabled -Ddvbin=disabled \
      -Drubberband=disabled -Dvapoursynth=disabled -Dlibavdevice=disabled \
      -Dsdl2-audio=disabled -Dsdl2-video=disabled -Dsdl2-gamepad=disabled \
      -Dopenal=disabled -Djack=disabled -Dpulse=disabled -Dpipewire=disabled -Dcaca=disabled -Dsixel=disabled \
      -Dx11=disabled -Dwayland=disabled -Ddrm=disabled -Degl=disabled \
      -Dvulkan=disabled -Dshaderc=disabled -Dspirv-cross=disabled -Dvideotoolbox-pl=disabled \
      -Dzimg=enabled -Dlcms2=enabled -Duchardet=enabled -Djpeg=enabled -Dzlib=enabled -Diconv=enabled \
      -Dgl=enabled -Dplain-gl=enabled -Dios-gl=enabled -Dvideotoolbox-gl=disabled \
      -Dcocoa=disabled -Dgl-cocoa=disabled -Dswift-build=disabled \
      -Dmacos-cocoa-cb=disabled -Dmacos-media-player=disabled -Dmacos-touchbar=disabled \
      -Dmacos-10-15-4-features=disabled -Dmacos-11-features=disabled -Dmacos-11-3-features=disabled \
      -Dmacos-12-features=disabled \
      -Daudiounit=enabled -Dcoreaudio=disabled -Davfoundation=disabled
  )
  local info="$DEST$PREFIX/share/nagare-libmpv"
  mkdir -p "$info"
  grep -E 'FULLCONFIG' "$OBJ/config.h" | sed 's/^#define FULLCONFIG //' > "$info/mpv-features.txt"
  grep -E '^#define HAVE_(GPL|IOS_GL|VIDEOTOOLBOX_GL|AUDIOUNIT|COREAUDIO|AVFOUNDATION|COCOA|LUA|CPLUGINS) ' \
    "$OBJ/config.h" | sort > "$info/mpv-config.txt"
  # 实际编进去的源文件（相对 mpv 源码根目录）：verify.sh 拿它对 Copyright 里的「GPL only」清单
  python3 -I -B -c '
import json, os, sys
src = os.path.realpath(sys.argv[2])
out = set()
for e in json.load(open(sys.argv[1])):
    f = os.path.realpath(os.path.join(e["directory"], e["file"]))
    if f.startswith(src + os.sep):
        out.add(os.path.relpath(f, src))
print("\n".join(sorted(out)))
' "$OBJ/compile_commands.json" "$SRC" > "$info/mpv-sources.txt"
  [ -s "$info/mpv-sources.txt" ] || die "读不出 mpv 编了哪些源文件（compile_commands.json）"
}
