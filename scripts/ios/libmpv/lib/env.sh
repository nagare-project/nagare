# shellcheck shell=bash
# iOS 目标的公共环境：在 macOS 那套（scripts/macos/libmpv/lib/env.sh）之上，换掉与目标平台有关的函数。
# 调用方要先引入 macOS 的 env.sh（log / die / isolate_pkg_config 等都来自那里），再引入本文件。
#
# 「架构」在这里是三个目标之一（build.sh --arch 收的就是它们）：
#   ios-arm64       真机（iphoneos SDK）
#   iossim-arm64    Apple Silicon 上的模拟器（iphonesimulator SDK）
#   iossim-x86_64   Intel Mac 上的模拟器（开发机是 Intel）
# 三者都编成静态库，package.sh 再把每个目标的静态库链成一个 Libmpv.framework，合成 xcframework。
#
# 与 macOS 版一样只用 bash 3.2 的语法。

IOS_LIBMPV_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
MACOS_LIBMPV_DIR=$(cd "$IOS_LIBMPV_DIR/../../macos/libmpv" && pwd)

IOS_TARGETS="ios-arm64 iossim-arm64 iossim-x86_64"

# 工作目录与 macOS 的分开（同一台机器上两套可以并存）；源码归档是同一批文件，可以用 LIBMPV_SOURCES 共用
WORK=${LIBMPV_WORK:-$REPO_ROOT/dist/libmpv-ios}
SOURCES_DIR=${LIBMPV_SOURCES:-$WORK/sources}

# 每个切片的 LC_BUILD_VERSION.minos 与 Libmpv.framework 的 MinimumOSVersion。
# App 的部署目标不能比它低；它定得低一点，App 那边怎么定都兼容。
DEPLOYMENT_TARGET=15.0
# macOS 的 env.sh 导出了 MACOSX_DEPLOYMENT_TARGET：iOS 的目标参数全写在 -target 里，
# 不能让它混进来（构建机上跑的代码生成器按 SDK 默认值编即可）
unset MACOSX_DEPLOYMENT_TARGET

check_arch() {
  case "$1" in
    ios-arm64 | iossim-arm64 | iossim-x86_64) ;;
    *) die "不支持的目标：${1}（只有 ${IOS_TARGETS}；用 --arch 指定）" ;;
  esac
}

# 目标的 CPU 架构（lipo / xcodebuild 的叫法）
target_cpu() {
  case "$1" in
    *-arm64) echo arm64 ;;
    *-x86_64) echo x86_64 ;;
  esac
}

# 目标的 SDK 名
target_sdk() {
  case "$1" in
    ios-*) echo iphoneos ;;
    iossim-*) echo iphonesimulator ;;
  esac
}

# clang 的 -target：模拟器必须带 -simulator 后缀 —— arm64 真机与 arm64 模拟器只靠它区分
target_triple() {
  case "$1" in
    ios-*) echo "$(target_cpu "$1")-apple-ios$DEPLOYMENT_TARGET" ;;
    iossim-*) echo "$(target_cpu "$1")-apple-ios$DEPLOYMENT_TARGET-simulator" ;;
  esac
}

# Info.plist 的 CFBundleSupportedPlatforms
target_platform_name() {
  case "$1" in
    ios-*) echo iPhoneOS ;;
    iossim-*) echo iPhoneSimulator ;;
  esac
}

# sdk_path [目标]（默认当前 $ARCH）。两种 SDK 各缓存一次：package.sh 会在一个进程里处理多个目标
sdk_path() {
  local sdk var
  sdk=$(target_sdk "${1:-$ARCH}")
  [ -n "$sdk" ] || die "sdk_path：不认识的目标 ${1:-$ARCH}"
  var="_SDK_PATH_$sdk"
  if [ -z "$(eval "printf '%s' \"\${$var:-}\"")" ]; then
    eval "$var=\$(xcrun --sdk \"\$sdk\" --show-sdk-path)" || die "xcrun 找不到 $sdk SDK"
  fi
  eval "printf '%s\n' \"\${$var}\""
}

# 编译与链接参数：-target 同时定下架构、平台（真机 / 模拟器）与部署目标
arch_flags() {
  echo "-target $(target_triple "$1") -isysroot $(sdk_path "$1")"
}

# autotools 的 --host。必须与 --build 不同，configure 才会进交叉编译模式（不去运行它编出来的测试程序）：
# 在 Intel Mac 上编 x86_64 模拟器时 CPU 相同，靠「ios」这个系统名区分
arch_triple() {
  case "$(target_cpu "$1")" in
    arm64) echo aarch64-apple-ios ;;
    x86_64) echo x86_64-apple-ios ;;
  esac
}

# autotools 的 --build：这台构建机
build_triple() {
  case "$HOST_ARCH" in
    arm64) echo aarch64-apple-darwin ;;
    x86_64) echo x86_64-apple-darwin ;;
  esac
}

# meson 的 cpu_family / FFmpeg 的 --arch
meson_cpu() {
  case "$(target_cpu "$1")" in
    arm64) echo aarch64 ;;
    x86_64) echo x86_64 ;;
  esac
}

# iOS 的程序在构建机上一个都跑不了（模拟器的也不行：要经 simctl 进模拟器才能跑）
can_run_arch() { return 1; }

toolchain_fingerprint() {
  if [ -z "${_TOOLCHAIN_FP:-}" ]; then
    _TOOLCHAIN_FP=$(
      {
        echo "platform ios deployment-target $DEPLOYMENT_TARGET"
        echo "host $HOST_ARCH"
        xcodebuild -version 2>/dev/null | tr '\n' ' '
        echo
        echo "sdk iphoneos $(xcrun --sdk iphoneos --show-sdk-version) iphonesimulator $(xcrun --sdk iphonesimulator --show-sdk-version)"
        clang --version | head -1
        echo "meson $(meson --version 2>/dev/null)"
        echo "ninja $(ninja --version 2>/dev/null)"
        cmake --version 2>/dev/null | head -1
        nasm -v 2>/dev/null
        echo "pkg-config $(pkg-config --version 2>/dev/null)"
        autoconf --version 2>/dev/null | head -1
        automake --version 2>/dev/null | head -1
        glibtool --version 2>/dev/null | head -1
      } 2>/dev/null
    )
  fi
  printf '%s\n' "$_TOOLCHAIN_FP"
}

# ---------------------------------------------------------------------------
# 构建助手（覆盖 macOS 版）。全部编静态库：每个目标最后链成一个动态 framework，
# 依赖库不再单独成为 dylib（App Store 不收散落的 dylib，23 个 framework 也没有必要）。
# ---------------------------------------------------------------------------

# meson 的交叉编译描述文件：iOS 永远是交叉编译
meson_machine_file() {
  local file="$OBJ/../meson-$ARCH.ini" list="" f lang subsystem=ios
  case "$ARCH" in iossim-*) subsystem=ios-simulator ;; esac
  for f in $(arch_flags "$ARCH"); do list="$list'$f', "; done
  {
    echo "[binaries]"
    echo "c = 'clang'"
    echo "cpp = 'clang++'"
    echo "objc = 'clang'"
    echo "objcpp = 'clang++'"
    echo "ar = 'ar'"
    echo "strip = 'strip'"
    echo "pkg-config = 'pkg-config'"
    echo "nasm = 'nasm'"
    echo
    echo "[built-in options]"
    for lang in c cpp objc objcpp; do
      echo "${lang}_args = [${list}'-I$PREFIX/include']"
      echo "${lang}_link_args = [${list}'-L$PREFIX/lib']"
    done
    echo
    echo "[properties]"
    echo "pkg_config_libdir = ['$PREFIX/lib/pkgconfig']"
    echo "needs_exe_wrapper = true"
    echo
    echo "[host_machine]"
    echo "system = 'darwin'"
    echo "subsystem = '$subsystem'"
    echo "kernel = 'xnu'"
    echo "cpu_family = '$(meson_cpu "$ARCH")'"
    echo "cpu = '$(meson_cpu "$ARCH")'"
    echo "endian = 'little'"
  } > "$file"
  echo "$file"
}

# meson_install <源码目录> [meson 选项...]
meson_install() {
  local src=$1
  shift
  local machine
  machine=$(meson_machine_file)
  isolate_pkg_config
  # nofallback：依赖找不到就直接失败，绝不让 meson 去下载 subprojects —— 所有源码都必须在 versions.lock 里
  env -u CFLAGS -u CXXFLAGS -u OBJCFLAGS -u LDFLAGS -u CPPFLAGS \
    meson setup "$OBJ" "$src" --cross-file="$machine" \
    --prefix="$PREFIX" --libdir=lib --buildtype=release --default-library=static \
    -Db_ndebug=true --wrap-mode=nofallback "$@"
  meson compile -C "$OBJ" -j "$JOBS"
  meson install -C "$OBJ" --destdir "$DEST" --no-rebuild
}

# cmake_install <源码目录> [cmake 选项...]
# CMake 按 CMAKE_OSX_SYSROOT 是 iPhoneOS 还是 iPhoneSimulator 选部署目标的参数（-miphoneos-version-min /
# -mios-simulator-version-min），平台不对的目标文件在 package.sh 链接时会直接报错。
cmake_install() {
  local src=$1
  shift
  isolate_pkg_config
  env -u CFLAGS -u CXXFLAGS -u LDFLAGS \
    cmake -S "$src" -B "$OBJ" -G Ninja \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="$PREFIX" \
    -DCMAKE_INSTALL_LIBDIR=lib \
    -DCMAKE_SYSTEM_NAME=iOS \
    -DCMAKE_OSX_SYSROOT="$(sdk_path "$ARCH")" \
    -DCMAKE_OSX_ARCHITECTURES="$(target_cpu "$ARCH")" \
    -DCMAKE_OSX_DEPLOYMENT_TARGET="$DEPLOYMENT_TARGET" \
    -DCMAKE_SYSTEM_PROCESSOR="$(meson_cpu "$ARCH")" \
    -DCMAKE_PREFIX_PATH="$PREFIX" \
    -DCMAKE_IGNORE_PREFIX_PATH="/opt/homebrew;/usr/local" \
    -DCMAKE_FIND_FRAMEWORK=LAST \
    -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
    -DBUILD_SHARED_LIBS=OFF \
    "$@"
  cmake --build "$OBJ" -j "$JOBS"
  DESTDIR="$DEST" cmake --install "$OBJ"
}

# autotools_install <源码目录> [configure 选项...]
autotools_install() {
  local src=$1
  shift
  local flags
  flags=$(arch_flags "$ARCH")
  isolate_pkg_config
  (
    cd "$OBJ"
    "$src/configure" \
      --prefix="$PREFIX" --libdir="$PREFIX/lib" \
      --host="$(arch_triple "$ARCH")" --build="$(build_triple)" \
      --enable-static --disable-shared --with-pic \
      CC=clang CXX=clang++ \
      CFLAGS="$flags -O2 -I$PREFIX/include" \
      CXXFLAGS="$flags -O2 -I$PREFIX/include" \
      LDFLAGS="$flags -L$PREFIX/lib" \
      "$@"
    make -j "$JOBS"
    make install DESTDIR="$DEST"
  )
}

# build.sh 把这些函数也算进缓存键（上面覆盖掉的同名函数本来就在它的清单里）
LIBMPV_EXTRA_KEY_FUNCTIONS="target_cpu target_sdk target_triple build_triple"
