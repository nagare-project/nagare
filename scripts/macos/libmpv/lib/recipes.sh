# shellcheck shell=bash
# 每个组件怎么编。build.sh 按 DEP_ORDER 的顺序逐个调用 recipe_<名字>。
#
# 每个组件声明三样东西：
#   <名字>_sources   versions.lock 里的源码名，第一个是主源码（解到 $SRC），其余是子模块
#   <名字>_deps      它要链接的其他组件（缓存键会带上它们的键：上游一变，下游跟着重编）
#   <名字>_licenses  主源码里要随二进制分发的许可证文件
# recipe_<名字> 的函数体本身也进缓存键 —— 改了编译参数，只重编这一个组件和依赖它的组件。
#
# 调用 recipe 时可用的变量：ARCH PREFIX SRC OBJ DEST（见 build.sh）。

DEP_ORDER="mbedtls dav1d zimg freetype fribidi harfbuzz libass lcms2 libplacebo libjpeg uchardet luajit ffmpeg mpv"

# --- 第一层：没有依赖的库 ----------------------------------------------------

# TLS：FFmpeg 的 https 用它。选型理由见 README「TLS」。
mbedtls_sources="mbedtls"
mbedtls_deps=""
mbedtls_licenses="LICENSE"
recipe_mbedtls() {
  cmake_install "$SRC" \
    -DENABLE_PROGRAMS=OFF -DENABLE_TESTING=OFF -DGEN_FILES=OFF \
    -DUSE_SHARED_MBEDTLS_LIBRARY=ON -DUSE_STATIC_MBEDTLS_LIBRARY=OFF \
    -DMBEDTLS_FATAL_WARNINGS=OFF
}

# AV1 软解
dav1d_sources="dav1d"
dav1d_deps=""
dav1d_licenses="COPYING"
recipe_dav1d() {
  meson_install "$SRC" \
    -Denable_tools=false -Denable_tests=false -Denable_examples=false -Denable_docs=false \
    -Dxxhash_muxer=disabled
}

# 高质量缩放 / 色彩转换：mpv 的截图与软件缩放，以及 thumbfast 给 HDR 片源做缩略图时的 zscale + tonemap
zimg_sources="zimg"
zimg_deps=""
zimg_licenses="COPYING"
recipe_zimg() {
  (cd "$SRC" && LIBTOOLIZE=glibtoolize autoreconf --install --force) >/dev/null
  # STL_LIBS 默认是 -lstdc++，新 SDK 里已经没有 libstdc++ 了
  autotools_install "$SRC" --disable-testapp --disable-example --disable-unit-test STL_LIBS=-lc++
}

# --- 字幕渲染（libass 与它的三个依赖） ---------------------------------------

freetype_sources="freetype"
freetype_deps=""
freetype_licenses="LICENSE.TXT docs/FTL.TXT docs/GPLv2.TXT"
recipe_freetype() {
  # harfbuzz 由 libass 自己接，这里不需要 freetype ↔ harfbuzz 的循环依赖；zlib 用系统的
  meson_install "$SRC" \
    -Dbrotli=disabled -Dbzip2=disabled -Dharfbuzz=disabled -Dpng=disabled \
    -Dzlib=system -Dtests=disabled
}

fribidi_sources="fribidi"
fribidi_deps=""
fribidi_licenses="COPYING"
recipe_fribidi() {
  meson_install "$SRC" -Ddocs=false -Dbin=false -Dtests=false
}

harfbuzz_sources="harfbuzz"
harfbuzz_deps=""
harfbuzz_licenses="COPYING"
recipe_harfbuzz() {
  # 只要核心整形库；libass 用自己的 FreeType 字体函数，不需要 hb-ft / CoreText 后端
  meson_install "$SRC" \
    -Dglib=disabled -Dgobject=disabled -Dcairo=disabled -Dchafa=disabled -Dpng=disabled \
    -Dzlib=disabled -Dicu=disabled -Dfreetype=disabled -Dcoretext=disabled \
    -Draster=disabled -Dvector=disabled -Dgpu=disabled -Dsubset=disabled \
    -Dtests=disabled -Dintrospection=disabled -Ddocs=disabled -Dutilities=disabled -Dbenchmark=disabled
}

libass_sources="libass"
libass_deps="freetype fribidi harfbuzz"
libass_licenses="COPYING"
recipe_libass() {
  # 字体走 CoreText（系统字体与回退），不带 fontconfig
  meson_install "$SRC" \
    -Dfontconfig=disabled -Dcoretext=enabled -Ddirectwrite=disabled -Dlibunibreak=disabled \
    -Drequire-system-font-provider=true -Dasm=enabled \
    -Dtest=disabled -Dcompare=disabled -Dprofile=disabled -Dfuzz=disabled -Dcheckasm=disabled
}

# --- 渲染 -------------------------------------------------------------------

lcms2_sources="lcms2"
lcms2_deps=""
lcms2_licenses="LICENSE"
recipe_lcms2() {
  # fastfloat / threaded 插件是 GPL-3.0，不编
  meson_install "$SRC" -Djpeg=disabled -Dtiff=disabled -Dtests=disabled -Dutils=false \
    -Dfastfloat=false -Dthreaded=false
}

# libplacebo 的 OpenGL 加载器由 glad 在构建时生成（要 jinja / markupsafe），fast_float 是头文件库。
# 这四个是上游仓库里的 git 子模块，版本与 libplacebo v7.360.1 钉的提交一致。
libplacebo_sources="libplacebo libplacebo/glad libplacebo/jinja libplacebo/markupsafe libplacebo/fast_float"
libplacebo_deps="lcms2"
libplacebo_licenses="LICENSE"
recipe_libplacebo() {
  local sub
  for sub in glad jinja markupsafe fast_float; do
    extract_source "libplacebo/$sub" "$SRC/3rdparty/$sub"
  done
  # render API 只有 OpenGL：不编 Vulkan / D3D11 / shaderc / glslang
  meson_install "$SRC" \
    -Dvulkan=disabled -Dopengl=enabled -Dgl-proc-addr=enabled -Dd3d11=disabled \
    -Dglslang=disabled -Dshaderc=disabled -Dlcms=enabled -Ddovi=enabled -Dlibdovi=disabled \
    -Dxxhash=disabled -Dunwind=disabled -Ddemos=false -Dtests=false -Dbench=false -Dfuzz=false
}

# --- 其余 mpv 直接用的库 -----------------------------------------------------

# 截图（mpv 默认 jpg）
libjpeg_sources="libjpeg-turbo"
libjpeg_deps=""
libjpeg_licenses="LICENSE.md README.ijg"
recipe_libjpeg() {
  cmake_install "$SRC" -DENABLE_STATIC=OFF -DENABLE_SHARED=ON -DWITH_TURBOJPEG=OFF \
    -DWITH_TOOLS=OFF -DWITH_TESTS=OFF -DREQUIRE_SIMD=ON
}

# 外挂字幕的编码探测（GBK / Shift-JIS / Big5 的老字幕）
uchardet_sources="uchardet"
uchardet_deps=""
uchardet_licenses="COPYING"
recipe_uchardet() {
  cmake_install "$SRC" -DBUILD_BINARY=OFF -DBUILD_STATIC=OFF -DCHECK_SSE2=OFF
}

# nagare 的播放器界面是 Lua 写的
luajit_sources="luajit"
luajit_deps=""
luajit_licenses="COPYRIGHT"
recipe_luajit() {
  # LuaJIT 只能在源码树里编。HOST_CC 编的是本机跑的代码生成器（minilua / buildvm），
  # 目标架构只经 TARGET_FLAGS 传进去；CFLAGS / LDFLAGS 会同时进两边，必须清空。
  local mk=(
    MACOSX_DEPLOYMENT_TARGET="$DEPLOYMENT_TARGET"
    PREFIX="$PREFIX"
    CC=clang HOST_CC=clang CFLAGS= LDFLAGS=
    TARGET_FLAGS="$(arch_flags "$ARCH")"
  )
  make -C "$SRC" -j "$JOBS" "${mk[@]}" amalg
  make -C "$SRC" "${mk[@]}" DESTDIR="$DEST" install
}

# --- FFmpeg -----------------------------------------------------------------

ff_list() { echo $1 | tr ' ' ','; }

ffmpeg_sources="ffmpeg"
ffmpeg_deps="mbedtls dav1d zimg"
ffmpeg_licenses="LICENSE.md COPYING.LGPLv2.1 COPYING.LGPLv3 COPYING.GPLv2 COPYING.GPLv3"
recipe_ffmpeg() {
  # 只开放番剧发布里会遇到的格式。清单与 README「FFmpeg 组件」对应，verify.sh 逐个核对。
  local FF_DECODERS="h264 hevc av1 libdav1d vp8 vp9 mpeg1video mpeg2video mpeg4 msmpeg4v1 msmpeg4v2 msmpeg4v3 h263 vc1 wmv3
    mjpeg png webp
    aac aac_latm ac3 eac3 truehd mlp dca flac opus vorbis mp1 mp1float mp2 mp2float mp3 mp3float alac
    pcm_s8 pcm_u8 pcm_s16le pcm_s16be pcm_s24le pcm_s24be pcm_s32le pcm_s32be pcm_f32le pcm_f32be pcm_f64le
    pcm_bluray pcm_dvd pcm_alaw pcm_mulaw
    aac_at ac3_at eac3_at mp3_at alac_at
    ass ssa subrip srt webvtt movtext text microdvd pgssub dvdsub dvbsub ccaption"
  local FF_DEMUXERS="matroska mov mpegts mpegps avi flv ogg hls wav flac mp3 aac ac3 eac3 dts dtshd truehd mlp h264 hevc
    ass srt webvtt sup vobsub microdvd image2 image_jpeg_pipe image_png_pipe image_webp_pipe"
  local FF_PARSERS="h264 hevc av1 vp8 vp9 mpeg4video mpegvideo vc1 h263 mjpeg png webp aac aac_latm ac3 dca flac mlp
    mpegaudio opus vorbis dvdsub dvbsub"
  # 截图（png / mjpeg；jpg 实际由 libjpeg 写）+ thumbfast 的 --ovc=rawvideo --of=image2
  local FF_ENCODERS="mjpeg png rawvideo"
  local FF_MUXERS="image2"
  local FF_PROTOCOLS="file http https httpproxy tcp tls crypto data"
  # mpv 自己的滤镜链（f_lavfi 的 buffer / buffersink 是 FFmpeg 内建，不用列）+ thumbfast 用到的
  # crop / scale / pad / format / hflip / vflip / zscale / tonemap + 去隔行与常见的音量整形
  local FF_FILTERS="crop pad scale format hflip vflip transpose rotate fps setpts null yadif bwdif zscale tonemap
    hwupload hwdownload yadif_videotoolbox scale_vt transpose_vt
    anull aformat aresample asetpts volume pan loudnorm dynaudnorm acompressor"
  local FF_HWACCELS="h264_videotoolbox hevc_videotoolbox vp9_videotoolbox av1_videotoolbox mpeg1_videotoolbox
    mpeg2_videotoolbox mpeg4_videotoolbox h263_videotoolbox"
  local flags cross=()
  flags="$(arch_flags "$ARCH") -I$PREFIX/include"
  [ "$ARCH" = "$HOST_ARCH" ] || cross=(--enable-cross-compile)
  isolate_pkg_config
  local args=(
    --prefix="$PREFIX" --libdir="$PREFIX/lib" --shlibdir="$PREFIX/lib"
    --target-os=darwin --arch="$ARCH" ${cross[@]+"${cross[@]}"}
    --cc=clang --cxx=clang++ --objcc=clang
    --extra-cflags="$flags" --extra-cxxflags="$flags" --extra-objcflags="$flags"
    --extra-ldflags="$(arch_flags "$ARCH") -L$PREFIX/lib"
    # mbedtls.pc 只给 -lmbedtls（x509 / crypto 在 Requires.private 里），动态链接要显式补上
    --extra-libs="-lmbedx509 -lmbedcrypto"
    --pkg-config=pkg-config
    --enable-shared --disable-static
    --disable-programs --disable-doc --disable-debug --disable-avdevice
    # 不加 --enable-gpl：这里没有任何 GPL 组件。--enable-version3 是 mbedTLS（Apache-2.0）要求的，
    # FFmpeg 因此整体为 LGPL-3.0-or-later。
    --enable-version3
    --enable-pthreads --enable-network --enable-zlib --enable-bzlib --enable-iconv
    --enable-videotoolbox --enable-audiotoolbox
    --enable-mbedtls --enable-libdav1d --enable-libzimg
    # 自动探测里不要的：TLS 只走 mbedTLS；不要设备 / 窗口 / X11 之类
    --disable-securetransport --disable-appkit --disable-avfoundation --disable-coreimage
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
  # 记下实际开出来的组件，进清单（manifest）
  local info="$DEST$PREFIX/share/nagare-libmpv"
  mkdir -p "$info"
  printf '%s\n' "${args[@]}" > "$info/ffmpeg-configure-args.txt"
  cp "$OBJ/configure.out" "$info/ffmpeg-configure-summary.txt"
  grep -E '^#define CONFIG_[A-Z0-9_]+_(DECODER|DEMUXER|PARSER|ENCODER|MUXER|PROTOCOL|FILTER|HWACCEL|BSF) 1' \
    "$OBJ/config_components.h" | awk '{print $2}' | sed 's/^CONFIG_//' | tr 'A-Z' 'a-z' | sort \
    > "$info/ffmpeg-components.txt"
}

# --- mpv --------------------------------------------------------------------

mpv_sources="mpv"
mpv_deps="ffmpeg libass libplacebo lcms2 libjpeg uchardet luajit zimg"
mpv_licenses="LICENSE.GPL LICENSE.LGPL Copyright"
recipe_mpv() {
  # 同一次构建出 libmpv（嵌进 Nagare 窗口）和命令行 mpv（thumbfast 与浏览器模式用）。
  # 命令行 mpv 在 macOS 上必须有 Swift（cocoa-cb 窗口）；它链接的是同一批依赖库，
  # 但 mpv 自身的代码是直接编进去的（meson 的 extract_all_objects），不经 libmpv.2.dylib。
  meson_install "$SRC" \
    -Dlibmpv=true -Dcplayer=true -Dgpl=true -Dbuild-date=false -Dtests=false -Dfuzzers=false \
    -Dmanpage-build=disabled -Dhtml-build=disabled -Dpdf-build=disabled \
    -Dlua=luajit -Djavascript=disabled \
    -Dlibarchive=disabled -Dlibbluray=disabled -Ddvdnav=disabled -Dcdda=disabled -Ddvbin=disabled \
    -Drubberband=disabled -Dvapoursynth=disabled -Dlibavdevice=disabled \
    -Dsdl2-audio=disabled -Dsdl2-video=disabled -Dsdl2-gamepad=disabled \
    -Dopenal=disabled -Djack=disabled -Dpulse=disabled -Dpipewire=disabled -Dcaca=disabled -Dsixel=disabled \
    -Dvulkan=disabled -Dshaderc=disabled -Dspirv-cross=disabled -Dvideotoolbox-pl=disabled \
    -Dzimg=enabled -Dlcms2=enabled -Duchardet=enabled -Djpeg=enabled -Dzlib=enabled -Diconv=enabled \
    -Dcocoa=enabled -Dgl=enabled -Dgl-cocoa=enabled -Dplain-gl=enabled -Dvideotoolbox-gl=enabled \
    -Dswift-build=enabled -Dmacos-cocoa-cb=enabled -Dmacos-media-player=enabled -Dmacos-touchbar=enabled \
    -Dcoreaudio=enabled -Davfoundation=enabled \
    -Dswift-flags="-target $ARCH-apple-macosx$DEPLOYMENT_TARGET"
  local info="$DEST$PREFIX/share/nagare-libmpv"
  mkdir -p "$info"
  grep -E 'FULLCONFIG' "$OBJ/config.h" | sed 's/^#define FULLCONFIG //' > "$info/mpv-features.txt"
  # 图标、补全脚本、示例配置都用不上
  rm -rf "$DEST$PREFIX/share/applications" "$DEST$PREFIX/share/icons" "$DEST$PREFIX/share/bash-completion" \
    "$DEST$PREFIX/share/zsh" "$DEST$PREFIX/share/fish" "$DEST$PREFIX/share/metainfo" "$DEST$PREFIX/share/doc"
}
