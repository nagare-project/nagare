#!/usr/bin/env bash
# 打 iOS 版 libmpv 的「完整对应源码」包（LGPL 要求随二进制提供库的源码与构建方法）：
# 这次构建用到的全部上游源码归档 + 构建脚本（iOS 的这一层与它复用的 macOS 构建流程、补丁、versions.lock）
# + CI 工作流。拿着这个包，在一台装了 Xcode 的 Mac 上就能重新构建出同一套 Libmpv.xcframework。
#
# 用法：source-bundle.sh <输出 .tar.gz 路径>
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
mac="$here/../../macos/libmpv"
# shellcheck source=../../macos/libmpv/lib/env.sh
. "$mac/lib/env.sh"
# shellcheck source=lib/env.sh
. "$here/lib/env.sh"
# shellcheck source=../../macos/libmpv/lib/sources.sh
. "$mac/lib/sources.sh"
# shellcheck source=../../macos/libmpv/lib/recipes.sh
. "$mac/lib/recipes.sh"
# shellcheck source=lib/recipes.sh
. "$here/lib/recipes.sh"

[ $# -eq 1 ] || die "用法：source-bundle.sh <输出 .tar.gz 路径>"
out=$1
mkdir -p "$(dirname "$out")"
out=$(cd "$(dirname "$out")" && pwd)/$(basename "$out")
name=$(basename "$out" .tar.gz)

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
root="$tmp/$name"
mkdir -p "$root/sources" "$root/scripts/macos" "$root/scripts/ios" "$root/.github/workflows"

for s in $(ios_used_sources); do
  fetch_source "$s"
  cp "$(source_archive "$s")" "$root/sources/"
done
# 两层脚本都整份带上：iOS 的构建是 macOS 那套流程加一层 overlay（补丁、versions.lock、测试片段都在 macOS 那边）
cp -R "$MACOS_LIBMPV_DIR" "$root/scripts/macos/libmpv"
cp -R "$IOS_LIBMPV_DIR" "$root/scripts/ios/libmpv"
cp "$REPO_ROOT/.github/workflows/libmpv-ios.yml" "$root/.github/workflows/"

commit=$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)
cat > "$root/README.txt" <<EOF
nagare 的 iOS libmpv（Libmpv.xcframework）：完整对应源码（LGPL-2.1-or-later 的 Corresponding Source）

来自 nagare 仓库（https://github.com/nagare-project/nagare）提交 ${commit}。
sources/                       这次构建用到的全部上游源码归档（逐个对 versions.lock 校验过 sha256）
scripts/ios/libmpv/            iOS 的构建层：目标参数、与 macOS 不同的组件配方、打包、验证、许可证选择
scripts/macos/libmpv/          复用的构建流程（build.sh）、源码版本锁（versions.lock）、mpv 补丁（patches/）、测试片段
.github/workflows/libmpv-ios.yml  CI 的构建与验证流程

重新构建（macOS + Xcode（CI 钉的版本见工作流）+ meson / ninja / cmake / nasm / pkg-config / autoconf /
automake / libtool，详见 scripts/ios/libmpv/README.md）：

  export LIBMPV_SOURCES=\$PWD/sources      # 用包里的源码，不再联网下载
  for t in ios-arm64 iossim-arm64 iossim-x86_64; do
    scripts/ios/libmpv/build.sh --arch \$t
    scripts/ios/libmpv/package.sh framework --target \$t
  done
  scripts/ios/libmpv/package.sh xcframework out
  scripts/ios/libmpv/verify.sh out

换掉库：Libmpv.framework 是动态链接的，App 包里的 Frameworks/Libmpv.framework 换成自己重新构建的那一份
（再按 Apple 的要求重新签名）即可。
EOF

COPYFILE_DISABLE=1 tar --no-xattrs -czf "$out" -C "$tmp" "$name"
log "源码包：${out}（$(du -h "$out" | awk '{print $1}')）"
