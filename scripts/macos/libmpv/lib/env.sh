# shellcheck shell=bash
# 公共环境与工具函数：路径、目标架构的编译参数、meson / cmake / autotools 的统一调用方式。
# 由 build.sh / fetch.sh / package.sh / verify.sh 引入（source），本身不执行任何动作。
#
# 只用 bash 3.2 的语法：macOS 自带的 /bin/bash 就是 3.2，GitHub 的 runner 也一样
# （没有关联数组、空数组在 set -u 下要写成 ${a[@]+"${a[@]}"}）。

LIBMPV_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
REPO_ROOT=$(cd "$LIBMPV_DIR/../../.." && pwd)

# 所有中间产物都在 WORK 下面（默认落在已被 .gitignore 的 dist/ 里）。CI 把它设到 runner 的临时目录，
# 路径必须每次相同：pkg-config 文件与动态库的装载名里写的是绝对路径，换了路径缓存就不能复用。
WORK=${LIBMPV_WORK:-$REPO_ROOT/dist/libmpv-macos}
SOURCES_DIR=${LIBMPV_SOURCES:-$WORK/sources}

# 与 Nagare.app 的 LSMinimumSystemVersion 一致（scripts/macos/Info.plist.tmpl）。
# 包里每一个 Mach-O 的 LC_BUILD_VERSION.minos 都不能高于它，verify.sh 会逐个检查。
DEPLOYMENT_TARGET=13.0
export MACOSX_DEPLOYMENT_TARGET=$DEPLOYMENT_TARGET

HOST_ARCH=$(uname -m)
JOBS=${LIBMPV_JOBS:-$(sysctl -n hw.ncpu)}

log() { printf '==> %s\n' "$*" >&2; }
warn() { printf '::warning::%s\n' "$*" >&2; }
die() { printf '错误：%s\n' "$*" >&2; exit 1; }

require_tools() {
  local t
  for t in "$@"; do
    command -v "$t" >/dev/null 2>&1 || die "缺少 ${t}（见 scripts/macos/libmpv/README.md「本地构建」）"
  done
}

sdk_path() {
  if [ -z "${_SDK_PATH:-}" ]; then
    _SDK_PATH=$(xcrun --sdk macosx --show-sdk-path) || die "xcrun 找不到 macOS SDK"
  fi
  printf '%s\n' "$_SDK_PATH"
}

check_arch() {
  case "$1" in
    arm64 | x86_64) ;;
    *) die "不支持的架构：${1}（只有 arm64 / x86_64）" ;;
  esac
}

# autotools 的 --host
arch_triple() {
  case "$1" in
    arm64) echo aarch64-apple-darwin ;;
    x86_64) echo x86_64-apple-darwin ;;
  esac
}

# meson 的 cpu_family
meson_cpu() {
  case "$1" in
    arm64) echo aarch64 ;;
    x86_64) echo x86_64 ;;
  esac
}

# 这台机器能不能直接跑该架构的程序（x86_64 在 Apple Silicon 上要靠 Rosetta）
can_run_arch() {
  [ "$1" = "$HOST_ARCH" ] && return 0
  [ "$1" = x86_64 ] && arch -x86_64 /usr/bin/true >/dev/null 2>&1
}

prefix_dir() { echo "$WORK/prefix/$1"; }
stage_root() { echo "$WORK/stage/$1"; }

# 给编译器的目标参数：架构 + 部署目标 + SDK。用 -isysroot 还有一个作用：
# 默认头文件搜索路径里的 /usr/local/include 会被换成 SDK 里的那份，runner 上散落的头文件进不来。
arch_flags() {
  echo "-arch $1 -mmacosx-version-min=$DEPLOYMENT_TARGET -isysroot $(sdk_path)"
}

# 工具链指纹：进缓存键。换了 Xcode / SDK / 构建工具，所有组件都重新编译。
toolchain_fingerprint() {
  if [ -z "${_TOOLCHAIN_FP:-}" ]; then
    _TOOLCHAIN_FP=$(
      {
        echo "deployment-target $DEPLOYMENT_TARGET"
        echo "host $HOST_ARCH"
        xcodebuild -version 2>/dev/null | tr '\n' ' '
        echo
        echo "sdk $(xcrun --sdk macosx --show-sdk-version)"
        clang --version | head -1
        xcrun swiftc --version 2>/dev/null | head -1
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
# 构建助手。调用前 build.sh 已设好：ARCH PREFIX SRC OBJ DEST（DEST = DESTDIR 暂存区）
# ---------------------------------------------------------------------------

# 只认 PREFIX 里的 .pc：runner 上 Homebrew 装的同名库一个都不能被找到。
isolate_pkg_config() {
  export PKG_CONFIG_LIBDIR="$PREFIX/lib/pkgconfig"
  export PKG_CONFIG_PATH=""
  unset PKG_CONFIG_SYSROOT_DIR
}

# meson 的机器描述文件。本机架构用 --native-file，另一个架构用 --cross-file。
# 两种都把编译参数写在文件里，所以调用 meson 时环境里不能再有 CFLAGS（否则会重复，交叉编译时还会
# 被 meson 当成「构建机」的参数，把 -arch 带进只该在本机跑的代码生成器里）。
meson_machine_file() {
  local file="$OBJ/../meson-$ARCH.ini" flags
  flags=$(arch_flags "$ARCH")
  local list="" f
  for f in $flags; do list="$list'$f', "; done
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
    local lang
    for lang in c cpp objc objcpp; do
      echo "${lang}_args = [${list}'-I$PREFIX/include']"
      echo "${lang}_link_args = [${list}'-L$PREFIX/lib']"
    done
    echo
    echo "[properties]"
    echo "pkg_config_libdir = ['$PREFIX/lib/pkgconfig']"
    if can_run_arch "$ARCH"; then echo "needs_exe_wrapper = false"; else echo "needs_exe_wrapper = true"; fi
    if [ "$ARCH" != "$HOST_ARCH" ]; then
      echo
      echo "[host_machine]"
      echo "system = 'darwin'"
      echo "subsystem = 'macos'"
      echo "kernel = 'xnu'"
      echo "cpu_family = '$(meson_cpu "$ARCH")'"
      echo "cpu = '$(meson_cpu "$ARCH")'"
      echo "endian = 'little'"
    fi
  } > "$file"
  echo "$file"
}

# meson_install <源码目录> [meson 选项...]
meson_install() {
  local src=$1
  shift
  local machine kind=--native-file
  machine=$(meson_machine_file)
  [ "$ARCH" = "$HOST_ARCH" ] || kind=--cross-file
  isolate_pkg_config
  # nofallback：依赖找不到就直接失败，绝不让 meson 去下载 subprojects —— 所有源码都必须在 versions.lock 里
  env -u CFLAGS -u CXXFLAGS -u OBJCFLAGS -u LDFLAGS -u CPPFLAGS \
    meson setup "$OBJ" "$src" "$kind=$machine" \
    --prefix="$PREFIX" --libdir=lib --buildtype=release --default-library=shared \
    -Db_ndebug=true --wrap-mode=nofallback "$@"
  meson compile -C "$OBJ" -j "$JOBS"
  meson install -C "$OBJ" --destdir "$DEST" --no-rebuild
}

# cmake_install <源码目录> [cmake 选项...]
cmake_install() {
  local src=$1
  shift
  local cross=()
  if [ "$ARCH" != "$HOST_ARCH" ]; then cross=(-DCMAKE_SYSTEM_NAME=Darwin); fi
  isolate_pkg_config
  # 装载名先写成 PREFIX 下的绝对路径（与 meson / autotools 的库一致），package.sh 再统一改成 @rpath。
  env -u CFLAGS -u CXXFLAGS -u LDFLAGS \
    cmake -S "$src" -B "$OBJ" -G Ninja \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="$PREFIX" \
    -DCMAKE_INSTALL_LIBDIR=lib \
    -DCMAKE_INSTALL_NAME_DIR="$PREFIX/lib" \
    -DCMAKE_OSX_ARCHITECTURES="$ARCH" \
    -DCMAKE_OSX_DEPLOYMENT_TARGET="$DEPLOYMENT_TARGET" \
    -DCMAKE_OSX_SYSROOT="$(sdk_path)" \
    -DCMAKE_SYSTEM_PROCESSOR="$(meson_cpu "$ARCH")" \
    -DCMAKE_PREFIX_PATH="$PREFIX" \
    -DCMAKE_IGNORE_PREFIX_PATH="/opt/homebrew;/usr/local" \
    -DCMAKE_FIND_FRAMEWORK=LAST \
    -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
    -DBUILD_SHARED_LIBS=ON \
    ${cross[@]+"${cross[@]}"} "$@"
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
      --host="$(arch_triple "$ARCH")" --build="$(arch_triple "$HOST_ARCH")" \
      --enable-shared --disable-static \
      CC=clang CXX=clang++ \
      CFLAGS="$flags -O2 -I$PREFIX/include" \
      CXXFLAGS="$flags -O2 -I$PREFIX/include" \
      LDFLAGS="$flags -L$PREFIX/lib" \
      "$@"
    make -j "$JOBS"
    make install DESTDIR="$DEST"
  )
}
