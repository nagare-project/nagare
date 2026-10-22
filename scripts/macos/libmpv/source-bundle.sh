#!/usr/bin/env bash
# 打「完整对应源码」包（GPL 的 Corresponding Source）：全部上游源码归档 + 我们的构建脚本、
# 补丁、versions.lock 与 CI 工作流。拿着这个包，在一台 macOS 上就能重新构建出同一套二进制。
#
# 用法：source-bundle.sh <输出 .tar.gz 路径>
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib/env.sh
. "$here/lib/env.sh"
# shellcheck source=lib/sources.sh
. "$here/lib/sources.sh"

[ $# -eq 1 ] || die "用法：source-bundle.sh <输出 .tar.gz 路径>"
out=$1
mkdir -p "$(dirname "$out")"
out=$(cd "$(dirname "$out")" && pwd)/$(basename "$out")
name=$(basename "$out" .tar.gz)

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
root="$tmp/$name"
mkdir -p "$root/sources" "$root/scripts/macos" "$root/.github/workflows"

for s in $(lock_names); do
  fetch_source "$s"
  cp "$(source_archive "$s")" "$root/sources/"
done
# 脚本目录整份带上（含 tests/ 的测试片段与 patches/）
cp -R "$LIBMPV_DIR" "$root/scripts/macos/libmpv"
cp "$REPO_ROOT/.github/workflows/libmpv-macos.yml" "$root/.github/workflows/"

commit=$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)
cat > "$root/README.txt" <<EOF
nagare 的 macOS libmpv 构建：完整对应源码（Corresponding Source）

来自 nagare 仓库（https://github.com/nagare-project/nagare）提交 ${commit}。
sources/                        versions.lock 列出的全部上游源码归档（逐个 sha256 校验过）
scripts/macos/libmpv/           构建脚本、补丁（patches/）、版本锁（versions.lock）、验证脚本与测试片段
.github/workflows/libmpv-macos.yml  CI 的构建与验证流程

重新构建（macOS，Xcode 命令行工具 + meson / ninja / cmake / nasm / pkg-config / autoconf /
automake / libtool，详见 scripts/macos/libmpv/README.md）：

  export LIBMPV_SOURCES=\$PWD/sources      # 用包里的源码，不再联网下载
  scripts/macos/libmpv/build.sh --arch arm64
  scripts/macos/libmpv/build.sh --arch x86_64
  scripts/macos/libmpv/package.sh thin --arch arm64
  scripts/macos/libmpv/package.sh thin --arch x86_64
  scripts/macos/libmpv/package.sh universal dist/libmpv-macos/thin/arm64 dist/libmpv-macos/thin/x86_64 out
EOF

# 整理一遍：不带 macOS 的扩展属性 / AppleDouble 文件
COPYFILE_DISABLE=1 tar --no-xattrs -czf "$out" -C "$tmp" "$name"
log "源码包：${out}（$(du -h "$out" | awk '{print $1}')）"
