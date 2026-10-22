#!/usr/bin/env bash
# 把 build.sh 的产物整理成可以直接放进 Nagare.app 的一套文件。
#
#   package.sh thin --arch <架构>            从 $WORK/prefix/<架构> 收集单架构的一套 → $WORK/thin/<架构>
#   package.sh universal <arm64 目录> <x86_64 目录> <输出目录>
#                                            两套 lipo 成 universal，ad-hoc 签名
#
# 产物布局（与 Nagare.app/Contents 对应，verify.sh 也直接在这个布局上跑）：
#   Frameworks/   libmpv.2.dylib 与它依赖的全部非系统动态库，都是真实文件（不留符号链接：
#                 nagare 的自更新拒绝符号链接），装载名一律 @rpath/<文件名>，没有 LC_RPATH
#   MacOS/mpv     命令行 mpv，唯一的 LC_RPATH 是 @executable_path/../Frameworks
#   include/mpv/  client.h render.h render_gl.h stream_cb.h
#   licenses/     各组件随二进制分发的许可证
#   info/<架构>/  FFmpeg 的配置参数与组件清单、mpv 的特性列表、构建工具链（进 manifest）
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib/env.sh
. "$here/lib/env.sh"

require_tools otool install_name_tool lipo codesign strip

# 系统库：只允许 /usr/lib 与 /System/Library 下的
is_system_path() {
  case "$1" in
    /usr/lib/* | /System/Library/*) return 0 ;;
  esac
  return 1
}

# Mach-O 的依赖（不含 LC_ID_DYLIB 自身）
macho_deps() {
  local file=$1 id=""
  if otool -D "$file" | sed -n 2p | grep -q .; then id=$(otool -D "$file" | sed -n 2p); fi
  otool -L "$file" | tail -n +2 | awk '{print $1}' | while read -r dep; do
    [ "$dep" = "$id" ] && continue
    echo "$dep"
  done
}

macho_rpaths() {
  otool -l "$1" | awk '/cmd LC_RPATH/{f=1} f && /path /{print $2; f=0}'
}

# install_name_tool / strip 改完文件都会提示「签名失效」，那是预期内的（最后统一重签）；
# 其余输出与失败照常报出来，不能吞掉
run_quiet_sig() {
  local out
  if ! out=$("$@" 2>&1); then
    printf '%s\n' "$out" >&2
    die "执行失败：$*"
  fi
  printf '%s\n' "$out" | grep -v -e 'invalidate the code signature' -e '^$' >&2 || true
}

thin() {
  local arch=$1 prefix out
  prefix=$(prefix_dir "$arch")
  out="$WORK/thin/$arch"
  [ -f "$prefix/lib/libmpv.2.dylib" ] || die "没有 $prefix/lib/libmpv.2.dylib，先跑 build.sh --arch $arch"
  [ -f "$prefix/bin/mpv" ] || die "没有 $prefix/bin/mpv"
  rm -rf "$out"
  mkdir -p "$out/Frameworks" "$out/MacOS" "$out/include/mpv" "$out/licenses" "$out/info/$arch"

  cp "$prefix/bin/mpv" "$out/MacOS/mpv"
  cp -L "$prefix/lib/libmpv.2.dylib" "$out/Frameworks/libmpv.2.dylib"

  # 从 libmpv 和 mpv 出发，顺着依赖把用到的库都收进来。不在 prefix 里、也不是系统库的依赖
  # （比如 runner 上 Homebrew 的库）直接报错 —— 那样的包装到用户机器上会起不来。
  local queue="$out/MacOS/mpv $out/Frameworks/libmpv.2.dylib" next file dep name
  while [ -n "$queue" ]; do
    next=""
    for file in $queue; do
      for dep in $(macho_deps "$file"); do
        is_system_path "$dep" && continue
        case "$dep" in
          "$prefix/lib/"*) name=$(basename "$dep") ;;
          @rpath/*) name=${dep#@rpath/} ;;
          *) die "$(basename "$file") 依赖了 prefix 之外的非系统库：$dep" ;;
        esac
        [ -e "$prefix/lib/$name" ] || die "$(basename "$file") 依赖的 $name 不在 $prefix/lib"
        if [ ! -f "$out/Frameworks/$name" ]; then
          cp -L "$prefix/lib/$name" "$out/Frameworks/$name"
          chmod 644 "$out/Frameworks/$name"
          next="$next $out/Frameworks/$name"
        fi
      done
    done
    queue=$next
  done

  # 改装载名：自身 → @rpath/<名字>，依赖 → @rpath/<名字>；删光 LC_RPATH，mpv 再加回包内那一条
  local args rp
  for file in "$out/MacOS/mpv" "$out/Frameworks/"*.dylib; do
    args=()
    case "$file" in *.dylib) args+=(-id "@rpath/$(basename "$file")") ;; esac
    for dep in $(macho_deps "$file"); do
      is_system_path "$dep" && continue
      args+=(-change "$dep" "@rpath/$(basename "$dep")")
    done
    for rp in $(macho_rpaths "$file" | sort -u); do args+=(-delete_rpath "$rp"); done
    if [ ${#args[@]} -gt 0 ]; then run_quiet_sig install_name_tool "${args[@]}" "$file"; fi
    # 同一条 LC_RPATH 重复出现时（cgo / swift 链接常见）一次只删得掉一条，删到没有为止
    while [ -n "$(macho_rpaths "$file")" ]; do
      run_quiet_sig install_name_tool -delete_rpath "$(macho_rpaths "$file" | head -1)" "$file"
    done
    # 去掉局部符号（导出符号保留），体积小一截；签名在 universal 那一步统一重做
    run_quiet_sig strip -x "$file"
  done
  run_quiet_sig install_name_tool -add_rpath @executable_path/../Frameworks "$out/MacOS/mpv"

  local h
  for h in client.h render.h render_gl.h stream_cb.h; do
    cp "$prefix/include/mpv/$h" "$out/include/mpv/$h"
  done
  cp -R "$prefix/share/licenses/." "$out/licenses/"
  cp -R "$prefix/share/nagare-libmpv/." "$out/info/$arch/"
  toolchain_fingerprint > "$out/info/$arch/toolchain.txt"
  log "单架构产物：$out（$(ls "$out/Frameworks" | wc -l | tr -d ' ') 个动态库）"
}

universal() {
  local a=$1 b=$2 out=$3
  [ -d "$a/Frameworks" ] && [ -d "$b/Frameworks" ] || die "用法：package.sh universal <arm64 目录> <x86_64 目录> <输出目录>"
  local la lb
  la=$(cd "$a" && find Frameworks MacOS -type f | sort)
  lb=$(cd "$b" && find Frameworks MacOS -type f | sort)
  [ "$la" = "$lb" ] || die "两个架构的文件清单不一致：$(diff <(echo "$la") <(echo "$lb") | tr '\n' ' ')"
  diff -r "$a/include" "$b/include" >/dev/null || die "两个架构的头文件不一致"
  diff -r "$a/licenses" "$b/licenses" >/dev/null || die "两个架构的许可证文件不一致"

  rm -rf "$out"
  mkdir -p "$out/Frameworks" "$out/MacOS" "$out/info"
  local f
  for f in $la; do
    lipo -create "$a/$f" "$b/$f" -output "$out/$f"
  done
  chmod 755 "$out/MacOS/mpv"
  cp -R "$a/include" "$a/licenses" "$out/"
  cp -R "$a/info/." "$b/info/." "$out/info/"

  # ad-hoc 签名：install_name_tool 与 lipo 都会让原签名失效，arm64 上没有有效签名的代码根本不让加载
  for f in $la; do
    codesign --force --sign - --timestamp=none "$out/$f"
  done
  for f in $la; do
    codesign --verify --strict "$out/$f" || die "签名校验失败：$f"
  done
  log "universal 产物：$out"
}

cmd=${1:-}
[ $# -gt 0 ] && shift
case "$cmd" in
  thin)
    [ "${1:-}" = --arch ] || die "用法：package.sh thin --arch <架构>"
    check_arch "$2"
    thin "$2"
    ;;
  universal) universal "$@" ;;
  *) sed -n '2,17p' "$0"; exit 2 ;;
esac
