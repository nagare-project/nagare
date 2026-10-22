#!/usr/bin/env bash
# 把 goreleaser 产出的 universal 二进制组装成 Nagare.app（ad-hoc 签名）并打成 dmg 与自更新用的 .app.zip。
# 由 .goreleaser.yaml 的 darwin-desktop-universal post hook 调用，也可手动运行。
#
# 用法：scripts/macos/build-app.sh <universal 二进制> <版本> <输出目录> <libmpv 目录> <libmpv 源码包>
#   <universal 二进制>  带 libmpv 构建标签的 nagare（桌面窗口，进程内链接 @rpath/libmpv.2.dylib）
#   <libmpv 目录>       nagare 自建的 libmpv（scripts/macos/libmpv/unpack.sh 解开、核对过的
#                       libmpv-macos-universal/：Frameworks/、MacOS/mpv、licenses/、manifest.json）
#   <libmpv 源码包>     同一次构建的完整对应源码（GPL），复制到 <输出目录>/mpv-sources/ 随 Release 发布
# 产物：<输出目录>/Nagare.app、nagare-<版本>_MacOS_universal.dmg、nagare-<版本>_MacOS_universal.app.zip
#
# 包里的布局：
#   Contents/MacOS/nagare                主程序；唯一的 LC_RPATH 是 @executable_path/../Frameworks
#   Contents/MacOS/mpv                   命令行 mpv（thumbfast 生成缩略图、--no-window 浏览器模式）
#   Contents/MacOS/nagare-source-<arch>  捆绑的 Nagare Source 引擎（单架构，运行时按 GOARCH 挑）
#   Contents/Frameworks/*.dylib          libmpv 与它的全部非系统依赖，真实文件（自更新拒绝符号链接）
#   Contents/Resources/libmpv/           各组件许可证原文与构建清单（GPL 要随二进制给）
#
# 零证书方案：ad-hoc 签名（identity "-"）没有 Apple 开发者证书，用户首次打开会被
# Gatekeeper 拦下，需要在「系统设置 › 隐私与安全性」里点「仍要打开」；README 有说明。
set -euo pipefail

if [ $# -ne 5 ]; then
  echo "用法：$0 <二进制路径> <版本> <输出目录> <libmpv 目录> <libmpv 源码包>" >&2
  exit 2
fi
bin=$1
version=$2
mkdir -p "$3"
out=$(cd "$3" && pwd)
lib=$4
libsrc=$5
root=$(cd "$(dirname "$0")/../.." && pwd)
macho=(python3 -I -B "$root/scripts/macos/libmpv/tools/macho.py")
rpath="@executable_path/../Frameworks"

for tool in codesign hdiutil plutil lipo otool install_name_tool ditto unzip python3; do
  command -v "$tool" >/dev/null || { echo "缺少 ${tool}（本脚本只能在 macOS 上运行）" >&2; exit 1; }
done
[ -f "$bin" ] || { echo "二进制不存在：$bin" >&2; exit 1; }
[ -f "$root/packaging/icon/nagare.icns" ] || { echo "缺少 packaging/icon/nagare.icns，先跑 scripts/icon/build-icons.sh" >&2; exit 1; }

# ── libmpv：没有、不完整、与源码包对不上，一律失败（不带着没核对过的 libmpv 出去，也不出没有它的 .app）──
for f in Frameworks/libmpv.2.dylib MacOS/mpv manifest.json MANIFEST.txt; do
  [ -f "$lib/$f" ] || { echo "libmpv 目录不完整（缺 ${f}）：$lib" >&2; exit 1; }
done
[ -d "$lib/licenses" ] || { echo "libmpv 目录不完整（缺 licenses/）：$lib" >&2; exit 1; }
# 开发期那份（IINA 的预编译库，scripts/macos/fetch-libmpv-dev.sh）绝不能进包：它没有我们的构建清单，
# 下面的核对本来就过不了；这里先给一句说得清楚的错误。
[ ! -e "$lib/DO-NOT-SHIP.txt" ] || { echo "这是开发期的 libmpv（IINA 的预编译库），不能打进发布包：$lib" >&2; exit 1; }
# 只收自建流水线的产物：每个文件都要与 manifest.json 记的大小、sha256 对得上，不多不少。
"${macho[@]}" verify-manifest "$lib" || { echo "libmpv 目录与它的 manifest.json 对不上：$lib" >&2; exit 1; }
[ -f "$libsrc" ] || { echo "libmpv 源码包不存在：$libsrc" >&2; exit 1; }
commit=$(plutil -extract source_commit raw -o - "$lib/manifest.json")
mpv_ver=$(plutil -extract components.0.version raw -o - "$lib/manifest.json")
want_src="libmpv-macos-source-${mpv_ver}-${commit:0:10}.tar.gz"
if [ "$(basename "$libsrc")" != "$want_src" ]; then
  echo "libmpv 源码包与这套 libmpv 不是同一次构建：期望 ${want_src}，实际 $(basename "$libsrc")" >&2
  exit 1
fi
echo "==> libmpv：mpv ${mpv_ver}，构建提交 ${commit:0:10}"

echo "==> 二进制架构：$(lipo -archs "$bin")"
for arch in $(lipo -archs "$bin"); do
  if ! otool -arch "$arch" -L "$bin" | grep -q '@rpath/libmpv.2.dylib'; then
    echo "二进制 ${arch} 没有链接 @rpath/libmpv.2.dylib —— 它不是桌面窗口版（构建时要带 -tags libmpv，见 .goreleaser.yaml 的 darwin-desktop）。" >&2
    exit 1
  fi
done

app="$out/Nagare.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources" "$app/Contents/Frameworks"
sed "s/__VERSION__/$version/g" "$root/scripts/macos/Info.plist.tmpl" > "$app/Contents/Info.plist"
plutil -lint "$app/Contents/Info.plist" >/dev/null
min_required=$(plutil -extract LSMinimumSystemVersion raw "$app/Contents/Info.plist")
printf 'APPL????' > "$app/Contents/PkgInfo"
cp "$bin" "$app/Contents/MacOS/nagare"
chmod 755 "$app/Contents/MacOS/nagare"
cp "$root/packaging/icon/nagare.icns" "$app/Contents/Resources/nagare.icns"

# 主程序只留一条 LC_RPATH：@executable_path/../Frameworks。.goreleaser.yaml 用 -extldflags 只加这一条，
# 但 cgo 经 CGO_LDFLAGS 加的 -rpath 会按 cgo 包的个数重复出现（原型里同一条构建目录 rpath 出现了三次），
# 构建目录的 rpath 还会让 dyld 在用户机器上去翻 /Users/runner/...。不对就逐个架构删光再加回这一条。
rpaths_of() { otool -arch "$2" -l "$1" | awk '/cmd LC_RPATH/{r=1} r&&/ path /{print $2; r=0}'; }
normalize_rpath() {
  local f=$1 arch thin first tmp ok=1
  for arch in $(lipo -archs "$f"); do
    [ "$(rpaths_of "$f" "$arch")" = "$rpath" ] || ok=0
  done
  [ "$ok" -eq 0 ] || return 0
  tmp=$(mktemp -d)
  for arch in $(lipo -archs "$f"); do
    echo "==> ${arch} 的 LC_RPATH 是 [$(rpaths_of "$f" "$arch" | paste -sd' ' -)]，改成只有 $rpath"
    thin="$tmp/$arch"
    lipo "$f" -thin "$arch" -output "$thin"
    for _ in $(seq 1 32); do
      first=$(rpaths_of "$thin" "$arch" | head -1)
      [ -n "$first" ] || break
      install_name_tool -delete_rpath "$first" "$thin"
    done
    install_name_tool -add_rpath "$rpath" "$thin"
  done
  lipo -create "$tmp"/* -output "$f"
  rm -rf "$tmp"
}
normalize_rpath "$app/Contents/MacOS/nagare"

# 自建 libmpv：dylib 与命令行 mpv 都复制成真实文件（自更新拒绝符号链接）。
echo "==> 复制 libmpv：$(find "$lib/Frameworks" -type f | wc -l | tr -d ' ') 个库 + 命令行 mpv"
for f in "$lib/Frameworks"/*; do
  [ -f "$f" ] && [ ! -L "$f" ] || { echo "Frameworks 里有不是普通文件的条目：$f" >&2; exit 1; }
  cp "$f" "$app/Contents/Frameworks/"
  chmod 644 "$app/Contents/Frameworks/$(basename "$f")"
done
cp "$lib/MacOS/mpv" "$app/Contents/MacOS/mpv"
chmod 755 "$app/Contents/MacOS/mpv"
# GPL / LGPL 要随二进制给许可证原文与说明；对应源码作为 Release 资产另外提供（见文件末尾）
mkdir -p "$app/Contents/Resources/libmpv"
cp -R "$lib/licenses" "$app/Contents/Resources/libmpv/licenses"
cp "$lib/MANIFEST.txt" "$lib/manifest.json" "$app/Contents/Resources/libmpv/"
cp "$root/LICENSE" "$root/THIRD_PARTY_NOTICES.md" "$app/Contents/Resources/"

# 捆绑的 Nagare Source 插件：引擎放 Contents/MacOS/nagare-source-<arch>，规则目录放
# Contents/Resources/nagare-source/repo —— bundle 规范要求 MacOS/ 下只能有可执行文件，
# 数据文件混进去 `codesign --verify --strict` 直接报 "code object is not signed at all"
# （CI 实测）。Go 侧（sourceplugin.DetectBundled）按这两个位置探测。
# lock 未钉版本时 fetch 脚本只留一个 README.txt，这里只复制资源目录，探测不到就当没捆。
plugin_src="$root/dist/source-plugin/MacOS_universal"
if [ -d "$plugin_src" ]; then
  mkdir -p "$app/Contents/Resources/nagare-source"
  for entry in "$plugin_src"/*; do
    case "$(basename "$entry")" in
      nagare-source-*)
        cp "$entry" "$app/Contents/MacOS/$(basename "$entry")"
        chmod 755 "$app/Contents/MacOS/$(basename "$entry")"
        ;;
      *) cp -R "$entry" "$app/Contents/Resources/nagare-source/" ;;
    esac
  done
fi

# 签名由里到外：每个 dylib、每个嵌套的可执行文件先各自 ad-hoc 签名，最后签 app（连同主程序）。
# 嵌套的不签，外层 --deep 校验不过；Gatekeeper 放行了 .app，子进程启动时仍会被拦。
echo "==> ad-hoc 签名"
for f in "$app/Contents/Frameworks"/*.dylib "$app/Contents/MacOS"/*; do
  [ "$f" = "$app/Contents/MacOS/nagare" ] && continue
  codesign --force --sign - "$f"
done
codesign --force --sign - "$app"
codesign --verify --deep --strict --verbose=2 "$app"

# 包里每个 Mach-O：universal（Nagare Source 引擎除外）、minos ≤ LSMinimumSystemVersion、
# 装载名 / LC_RPATH / 依赖都落在包里、签名有效、没有符号链接。
#
# minos 为什么要查：cgo 外部链接时 clang 会把构建机 SDK 的版本写进 LC_BUILD_VERSION.minos。
# 构建机（CI 的 macos-latest）总是比用户的系统新，不钉死就会产出「只能在最新 macOS 上
# 启动」的 app，而且失败发生在用户双击的那一刻（kLSIncompatibleSystemVersionErr），
# CI 全绿也发现不了。dylib 与命令行 mpv 同理，只是失败发生在第一次播放的那一刻。
echo "==> 检查包里的每个 Mach-O（最低系统版本 ${min_required}）"
if ! "${macho[@]}" bundle "$app" --minos "$min_required"; then
  echo "构建时请设置 MACOSX_DEPLOYMENT_TARGET / CGO_CFLAGS / CGO_LDFLAGS 为 ${min_required}（见 .goreleaser.yaml）；libmpv 的部署目标见 scripts/macos/libmpv/README.md。" >&2
  exit 1
fi

dmg="$out/nagare-${version}_MacOS_universal.dmg"
rm -f "$dmg"
stage=$(mktemp -d)
# trap 里要显式保住退出码：bash 3.2（macOS runner 自带的就是它）在 EXIT trap
# 跑完之后会用 trap 的退出码顶掉脚本自己的 —— 脚本失败了 CI 却是绿的。
trap 'ec=$?; rm -rf "$stage"; exit "$ec"' EXIT
cp -R "$app" "$stage/"
ln -s /Applications "$stage/Applications"

echo "==> 打包 dmg：$dmg"
# GitHub 的 macOS runner 上 hdiutil 偶发 "Resource busy"，重试三次。
for attempt in 1 2 3; do
  if hdiutil create -volname nagare -srcfolder "$stage" -ov -format UDZO -quiet "$dmg"; then
    break
  fi
  [ "$attempt" -lt 3 ] || { echo "hdiutil create 连续失败" >&2; exit 1; }
  echo "hdiutil create 失败，${attempt}s 后重试…" >&2
  sleep "$attempt"
done

echo "==> 验证 dmg 可挂载且签名为 adhoc"
mnt=$(mktemp -d)
hdiutil attach "$dmg" -nobrowse -readonly -quiet -mountpoint "$mnt"
sig=$(codesign -dv "$mnt/Nagare.app" 2>&1 || true)
deep=0
codesign --verify --deep --strict "$mnt/Nagare.app" 2>/dev/null || deep=1
hdiutil detach "$mnt" -quiet
rmdir "$mnt" 2>/dev/null || true
if ! grep -q 'Signature=adhoc' <<<"$sig"; then
  echo "dmg 内 Nagare.app 的签名不是 adhoc：" >&2
  echo "$sig" >&2
  exit 1
fi
[ "$deep" -eq 0 ] || { echo "dmg 内 Nagare.app 的签名校验不过（--deep --strict）" >&2; exit 1; }
# 自更新要用的 .app 整包压缩。
#
# 为什么不是「只替换 .app 里的那个二进制」：bundle 是 ad-hoc 签名的，换掉里面的
# 可执行文件会让签名失效，macOS 直接拒绝启动。自更新只能整包替换，所以这里必须
# 单独出一个 .app 的 zip（dmg 也能用，但要 hdiutil 挂载，客户端侧复杂得多）。
appzip="$out/nagare-${version}_MacOS_universal.app.zip"
rm -f "$appzip"
echo "==> 打包 .app zip（自更新用）：$appzip"
# 用 ditto 而不是 zip：只有它完整保留 bundle 的符号链接、扩展属性与代码签名。
# 普通 zip 压出来的包解开后 codesign 校验不过，自更新装上去就是个打不开的 app。
(cd "$out" && ditto -c -k --sequesterRsrc --keepParent Nagare.app "$appzip")

# 验证 zip 往返之后签名仍然成立。
#
# 【两种解包方式都要验】，这不是重复：
#   ditto -x  是 Apple 自己的解包器，会把 __MACOSX/ 里 sequester 的扩展属性与资源叉
#             还原回去；
#   unzip     只还原普通文件，与客户端实际用的 Go archive/zip 是同一档能力。
# 只验前者的话，一旦签名材料哪天落到扩展属性上，CI 会一直是绿的，而每个用户
# 自更新出来的都是打不开的 app —— 失败发生在最看不见的地方。
# 解开后再跑一遍整包检查：Frameworks 里的库、命令行 mpv 的可执行位都要原样回来。
# （客户端那套 Go 解包代码本身由 release.yml 的「自更新解包往返」一步另外跑。）
echo "==> 验证 zip 往返之后签名仍然成立（ditto 与 unzip 两种解包）"
for extractor in ditto unzip; do
  zipcheck=$(mktemp -d)
  case "$extractor" in
    ditto) ditto -x -k "$appzip" "$zipcheck" ;;
    unzip) unzip -qq "$appzip" -d "$zipcheck" ;;
  esac
  if ! codesign --verify --deep --strict "$zipcheck/Nagare.app" 2>/dev/null; then
    echo "用 ${extractor} 解包后 Nagare.app 的签名校验不过 —— 自更新装上去会打不开。" >&2
    codesign --verify --deep --strict --verbose=2 "$zipcheck/Nagare.app" >&2 || true
    rm -rf "$zipcheck"
    exit 1
  fi
  if ! "${macho[@]}" bundle "$zipcheck/Nagare.app" --minos "$min_required" >/dev/null; then
    echo "用 ${extractor} 解包后 Nagare.app 的整包检查不过：" >&2
    "${macho[@]}" bundle "$zipcheck/Nagare.app" --minos "$min_required" >&2 || true
    rm -rf "$zipcheck"
    exit 1
  fi
  echo "==> ${extractor} 解包后签名有效、整包检查通过 ✓"
  rm -rf "$zipcheck"
done

# GPL：这套 libmpv 的完整对应源码与二进制一起发（同 Windows 的 mpv-sources/，checksums 与 Release 都收它）
mkdir -p "$out/mpv-sources"
cp "$libsrc" "$out/mpv-sources/$want_src.part"
mv "$out/mpv-sources/$want_src.part" "$out/mpv-sources/$want_src"

echo "==> 完成：${app}（$(du -sh "$app" | awk '{print $1}')）"
echo "==> 完成：${dmg}（$(du -h "$dmg" | awk '{print $1}')）"
echo "==> 完成：${appzip}（$(du -h "$appzip" | awk '{print $1}')）"
echo "==> 完成：$out/mpv-sources/$want_src"
