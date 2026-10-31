#!/usr/bin/env bash
# 把 build.sh 的静态库产物整理成 App 能直接用的 Libmpv.xcframework。
#
#   package.sh framework --target <目标>   $WORK/prefix/<目标> 的静态库 → 一个动态的 Libmpv.framework
#                                          （→ $WORK/fw/<目标>/，连同 info/、licenses/、include/）
#   package.sh xcframework <输出目录>       三个目标的 framework 合成 Libmpv.xcframework（模拟器的 arm64 与
#                                          x86_64 先 lipo 成一份）；FW_ROOT 改 framework 所在目录（默认 $WORK/fw）
#
# 输出目录布局（verify.sh 直接在它上面跑）：
#   Libmpv.xcframework/   ios-arm64/Libmpv.framework 与 ios-arm64_x86_64-simulator/Libmpv.framework
#   include/mpv/          四个头文件（Go 的 cgo 用 -I 指到这里；framework 里也有一份，Swift 用 module map）
#   licenses/             各组件随二进制分发的许可证（只有 LGPL 与更宽松的）
#   info/<目标>/          FFmpeg / mpv / LuaJIT 的配置记录、链接日志、工具链
#
# 为什么是一个动态 framework：LGPL 要让用户能换掉这个库再链接；动态链接时换掉 framework 即可，
# 依赖全部静态并进来，App 里只多一个 framework（App Store 不收散落的 dylib）。只导出 mpv_* 符号。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=../../macos/libmpv/lib/env.sh
. "$here/../../macos/libmpv/lib/env.sh"
# shellcheck source=lib/env.sh
. "$here/lib/env.sh"
# shellcheck source=../../macos/libmpv/lib/sources.sh
. "$here/../../macos/libmpv/lib/sources.sh"

require_tools clang lipo strip codesign xcodebuild pkg-config plutil

FW_ROOT=${FW_ROOT:-$WORK/fw}
BUNDLE_ID=io.github.nagare-project.Libmpv

framework_plist() {
  local target=$1 version=$2
  cat <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleExecutable</key>
	<string>Libmpv</string>
	<key>CFBundleIdentifier</key>
	<string>$BUNDLE_ID</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>Libmpv</string>
	<key>CFBundlePackageType</key>
	<string>FMWK</string>
	<key>CFBundleShortVersionString</key>
	<string>$version</string>
	<key>CFBundleVersion</key>
	<string>$version</string>
	<key>CFBundleSupportedPlatforms</key>
	<array>
		<string>$(target_platform_name "$target")</string>
	</array>
	<key>MinimumOSVersion</key>
	<string>$DEPLOYMENT_TARGET</string>
</dict>
</plist>
EOF
}

framework() {
  local target=$1 prefix out fw version api libs
  prefix=$(prefix_dir "$target")
  out="$FW_ROOT/$target"
  fw="$out/Libmpv.framework"
  [ -f "$prefix/lib/libmpv.a" ] || die "没有 $prefix/lib/libmpv.a，先跑 build.sh --arch $target"
  rm -rf "$out"
  mkdir -p "$fw/Headers/mpv" "$fw/Modules" "$out/include/mpv" "$out/licenses" "$out/info/$target"

  version=$(lock_field mpv 2)
  export PKG_CONFIG_LIBDIR="$prefix/lib/pkgconfig" PKG_CONFIG_PATH=""
  api=$(pkg-config --modversion mpv)
  # 全部依赖（Requires.private 递归展开）：静态库、系统库与 framework
  libs=$(pkg-config --static --libs mpv)
  case " $libs " in *" -lstdc++ "*) die "链接参数里有 -lstdc++（iOS SDK 没有 libstdc++）：$libs" ;; esac
  printf '%s\n' "$libs" > "$out/info/$target/link-libs.txt"

  printf '_mpv_*\n' > "$out/exports.txt"
  log "${target}：链接 Libmpv.framework（mpv ${version}，client API ${api}）"
  # -force_load 把 libmpv.a 整个拉进来（只导出 mpv_*，否则链接器不会去静态库里取任何东西），
  # 其余静态库按需取；-dead_strip 去掉从 mpv_* 走不到的代码。C++ 的依赖（libplacebo、zimg、uchardet、
  # harfbuzz）要 libc++。
  # shellcheck disable=SC2046,SC2086 # arch_flags 与 pkg-config 的输出故意按空白拆开
  if ! clang -dynamiclib $(arch_flags "$target") \
    -install_name @rpath/Libmpv.framework/Libmpv \
    -compatibility_version 1.0.0 -current_version "$api" \
    -Wl,-exported_symbols_list,"$out/exports.txt" -Wl,-dead_strip \
    -Wl,-force_load,"$prefix/lib/libmpv.a" $libs -lc++ \
    -o "$fw/Libmpv" > "$out/info/$target/link.log" 2>&1; then
    cat "$out/info/$target/link.log" >&2
    die "${target}：链接失败"
  fi
  if [ -s "$out/info/$target/link.log" ]; then sed 's/^/  ld | /' "$out/info/$target/link.log" >&2; fi
  # 有目标文件的平台或最低版本与这个切片不一致（某个组件没按 iOS 的参数编）：链接器只警告，这里当错误
  if grep -E -i 'built for|was built for newer|building for .* but linking' "$out/info/$target/link.log" >&2; then
    die "${target}：有目标文件不是按这个切片的平台 / 部署目标编的"
  fi
  strip -x "$fw/Libmpv" 2>&1 | grep -v 'invalidate the code signature' >&2 || true

  local h
  for h in client.h render.h render_gl.h stream_cb.h; do
    cp "$prefix/include/mpv/$h" "$fw/Headers/mpv/$h"
    cp "$prefix/include/mpv/$h" "$out/include/mpv/$h"
  done
  cat > "$fw/Modules/module.modulemap" <<'EOF'
framework module Libmpv {
    header "mpv/client.h"
    header "mpv/render.h"
    header "mpv/render_gl.h"
    header "mpv/stream_cb.h"
    export *
}
EOF
  framework_plist "$target" "$version" > "$fw/Info.plist"
  plutil -lint "$fw/Info.plist" >/dev/null || die "Info.plist 格式不对"
  codesign --force --sign - --timestamp=none "$fw"

  cp -R "$prefix/share/licenses/." "$out/licenses/"
  cp -R "$prefix/share/nagare-libmpv/." "$out/info/$target/"
  toolchain_fingerprint > "$out/info/$target/toolchain.txt"
  log "${target}：$(du -h "$fw/Libmpv" | awk '{print $1}')  $fw"
}

xcframework() {
  local out=$1 t sim
  for t in $IOS_TARGETS; do
    [ -f "$FW_ROOT/$t/Libmpv.framework/Libmpv" ] || die "缺少 $FW_ROOT/$t/Libmpv.framework，先跑 package.sh framework --target $t"
  done
  # 三个目标的头文件与许可证必须完全相同（同一份源码、同一套配方）
  for t in iossim-arm64 iossim-x86_64; do
    diff -r "$FW_ROOT/ios-arm64/include" "$FW_ROOT/$t/include" >/dev/null || die "$t 的头文件与 ios-arm64 不同"
    diff -r "$FW_ROOT/ios-arm64/licenses" "$FW_ROOT/$t/licenses" >/dev/null || die "$t 的许可证文件与 ios-arm64 不同"
  done
  diff "$FW_ROOT/iossim-arm64/Libmpv.framework/Info.plist" "$FW_ROOT/iossim-x86_64/Libmpv.framework/Info.plist" \
    >/dev/null || die "两个模拟器目标的 Info.plist 不同"

  # xcframework 里同一平台只能有一份：模拟器的两个架构合成一个 fat framework
  sim="$FW_ROOT/iossim"
  rm -rf "$sim"
  mkdir -p "$sim"
  cp -R "$FW_ROOT/iossim-arm64/Libmpv.framework" "$sim/"
  lipo -create "$FW_ROOT/iossim-arm64/Libmpv.framework/Libmpv" "$FW_ROOT/iossim-x86_64/Libmpv.framework/Libmpv" \
    -output "$sim/Libmpv.framework/Libmpv"
  codesign --force --sign - --timestamp=none "$sim/Libmpv.framework"

  rm -rf "$out"
  mkdir -p "$out/info"
  xcodebuild -create-xcframework \
    -framework "$FW_ROOT/ios-arm64/Libmpv.framework" \
    -framework "$sim/Libmpv.framework" \
    -output "$out/Libmpv.xcframework" >&2
  cp -R "$FW_ROOT/ios-arm64/include" "$FW_ROOT/ios-arm64/licenses" "$out/"
  for t in $IOS_TARGETS; do
    cp -R "$FW_ROOT/$t/info/." "$out/info/"
  done
  log "xcframework：$out/Libmpv.xcframework"
}

cmd=${1:-}
[ $# -gt 0 ] && shift
case "$cmd" in
  framework)
    [ "${1:-}" = --target ] && [ -n "${2:-}" ] || die "用法：package.sh framework --target <目标>"
    check_arch "$2"
    framework "$2"
    ;;
  xcframework)
    [ $# -eq 1 ] || die "用法：package.sh xcframework <输出目录>"
    xcframework "$1"
    ;;
  *) sed -n '2,17p' "$0"; exit 2 ;;
esac
