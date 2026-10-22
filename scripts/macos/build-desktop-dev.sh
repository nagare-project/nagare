#!/usr/bin/env bash
# 本机开发用：带 macOS 桌面窗口（构建标签 libmpv）编一个 nagare，链接 .libmpv-dev/ 里的开发期 libmpv。
#
# ##  仅供本机开发。产物链接的是 IINA 的预编译 dylib，绝不能拿去发布。  ##
#
# 用法：scripts/macos/build-desktop-dev.sh [输出路径] [额外 go build 参数…]
#   默认输出 ./nagare-desktop-dev；先跑 scripts/macos/fetch-libmpv-dev.sh。
#   想要内嵌界面，先 (cd frontend && bun run build)，否则是 API-only。
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
out=${1:-$root/nagare-desktop-dev}
shift || true
[ -f "$root/.libmpv-dev/lib/libmpv.2.dylib" ] || { echo "先跑 scripts/macos/fetch-libmpv-dev.sh" >&2; exit 1; }

eval "$("$root/scripts/macos/fetch-libmpv-dev.sh" --print-env)"
# 与 .goreleaser.yaml 的 darwin 构建一致：最低 macOS 13
export MACOSX_DEPLOYMENT_TARGET=13.0
export CGO_ENABLED=1 CGO_CFLAGS="$CGO_CFLAGS -mmacosx-version-min=13.0" CGO_LDFLAGS="$CGO_LDFLAGS -mmacosx-version-min=13.0"
# 本机的 CommandLineTools SDK 太旧：net/http 引用的 macOS 12 符号 SecTrustCopyCertificateChain
# 不在 SDK 11 的 tbd 里。-U 让它留到运行时再解析（macOS 12 起系统里都有），只影响本机开发构建。
ldflags="-extldflags=-Wl,-U,_SecTrustCopyCertificateChain"
cd "$root"
go build -tags libmpv -ldflags="$ldflags" -o "$out" "$@" .
echo "==> ${out}（运行时从 ${root}/.libmpv-dev/lib 加载 libmpv）"
