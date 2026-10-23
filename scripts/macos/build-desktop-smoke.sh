#!/usr/bin/env bash
# 编桌面窗口的测试构建（-tags "libmpv desktoptest"，带 internal/desktop/testctl_darwin.* 的遥控），链接 nagare
# 自建的 libmpv，并按 Nagare.app/Contents 的布局放好 —— scripts/macos/desktop-smoke.py 跑的就是它：
#   <输出>/MacOS/nagare      唯一的 LC_RPATH 是 @executable_path/../Frameworks（与发布版一样）
#   <输出>/MacOS/mpv         命令行 mpv（探测为「内置」，thumbfast 用它）
#   <输出>/Frameworks/*.dylib
# 只给 CI 与本机测试用，不签名、不打包，不进任何 Release。
#
# 用法：scripts/macos/build-desktop-smoke.sh <libmpv 目录（unpack.sh 解开的 libmpv-macos-universal）> <输出目录>
# 环境：GOARCH 默认本机架构；SMOKE_EXTLDFLAGS 追加给外部链接器（本机 CommandLineTools SDK 太旧时传
#       -Wl,-U,_SecTrustCopyCertificateChain，见 scripts/macos/build-desktop-dev.sh）。
# 前端要先编好（web/ 为空时 nagare 是 API-only，不开窗口）。
set -euo pipefail

[ $# -eq 2 ] || { echo "用法：$0 <libmpv 目录> <输出目录>" >&2; exit 2; }
lib=$(cd "$1" && pwd)
out=$2
root=$(cd "$(dirname "$0")/../.." && pwd)
[ -f "$lib/Frameworks/libmpv.2.dylib" ] && [ -f "$lib/MacOS/mpv" ] && [ -f "$lib/include/mpv/client.h" ] \
  || { echo "不是 libmpv-macos-universal 目录：$lib" >&2; exit 1; }
[ -f "$root/web/index.html" ] || { echo "web/ 里没有前端产物，先 (cd frontend && bun run build)" >&2; exit 1; }

rm -rf "$out"
mkdir -p "$out/MacOS" "$out/Frameworks"
out=$(cd "$out" && pwd)
cp "$lib"/Frameworks/*.dylib "$out/Frameworks/"
cp "$lib/MacOS/mpv" "$out/MacOS/mpv"
chmod 755 "$out/MacOS/mpv"

export CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=13.0
export CGO_CFLAGS="-mmacosx-version-min=13.0 -I$lib/include"
export CGO_LDFLAGS="-mmacosx-version-min=13.0 -L$lib/Frameworks"
extld="-Wl,-rpath,@executable_path/../Frameworks${SMOKE_EXTLDFLAGS:+ $SMOKE_EXTLDFLAGS}"
cd "$root"
go build -tags "libmpv desktoptest" -trimpath -ldflags="-X main.version=0.0.0-smoke '-extldflags=$extld'" \
  -o "$out/MacOS/nagare" .
otool -L "$out/MacOS/nagare" | grep -q '@rpath/libmpv.2.dylib' || { echo "测试构建没有链接 libmpv" >&2; exit 1; }
echo "==> $out/MacOS/nagare（$(lipo -archs "$out/MacOS/nagare")）"
