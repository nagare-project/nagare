#!/usr/bin/env bash
# 核对 source-bundle.sh 打出来的对应源码包真的够重新构建：LGPL 的义务落在这个包上，光打出来不验等于没给。
#   * 这次构建用到的每一份上游源码都在 sources/ 里、sha256 与 versions.lock 一致，没有多余的
#   * 两层构建脚本、补丁、版本锁、许可证选择、CI 工作流与 README.txt 都在，且 versions.lock 与仓库里的相同
#   * 用包里的脚本 + 包里的源码（LIBMPV_SOURCES=<包>/sources）算出的三个目标的缓存键，与用仓库算的一致：
#     缓存键覆盖了全部配方、构建助手、补丁、源码锁与工具链，相等就说明包里的东西能编出同一套产物
#
# 用法：check-source-bundle.sh <源码包 .tar.gz>（要在仓库里跑；第三项需要 build.sh 要的那些构建工具）
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
mac="$here/../../macos/libmpv"
# shellcheck source=../../macos/libmpv/lib/env.sh
. "$mac/lib/env.sh"
# shellcheck source=../../macos/libmpv/lib/sources.sh
. "$mac/lib/sources.sh"
# shellcheck source=../../macos/libmpv/lib/recipes.sh
. "$mac/lib/recipes.sh"
# shellcheck source=lib/recipes.sh
. "$here/lib/recipes.sh"

[ $# -eq 1 ] && [ -f "$1" ] || die "用法：check-source-bundle.sh <源码包 .tar.gz>"
bundle=$1
name=$(basename "$bundle" .tar.gz)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

listing=$(tar -tzf "$bundle") || die "读不了 ${bundle}（截断了？）"
if grep -v -e "^${name}/" -e "^${name}\$" <<<"$listing" | grep -q .; then die "包里有 ${name}/ 之外的条目"; fi
tar -xzf "$bundle" -C "$tmp"
root="$tmp/$name"

bad=0
for f in README.txt .github/workflows/libmpv-ios.yml \
  scripts/ios/libmpv/build.sh scripts/ios/libmpv/package.sh scripts/ios/libmpv/verify.sh \
  scripts/ios/libmpv/license-choices.txt scripts/ios/libmpv/lib/env.sh scripts/ios/libmpv/lib/recipes.sh \
  scripts/macos/libmpv/build.sh scripts/macos/libmpv/versions.lock scripts/macos/libmpv/lib/env.sh \
  scripts/macos/libmpv/lib/sources.sh scripts/macos/libmpv/lib/recipes.sh; do
  [ -f "$root/$f" ] || { echo "  ✗ 包里缺少 $f"; bad=1; }
done
cmp -s "$root/scripts/macos/libmpv/versions.lock" "$LOCK_FILE" || { echo "  ✗ 包里的 versions.lock 与仓库的不同"; bad=1; }
for p in "$mac"/patches/*/*.patch; do
  rel=${p#"$mac"/}
  cmp -s "$p" "$root/scripts/macos/libmpv/$rel" || { echo "  ✗ 包里缺少或改了补丁 $rel"; bad=1; }
done

n_used=0
for s in $(ios_used_sources); do
  n_used=$((n_used + 1))
  f="$root/sources/$(lock_field "$s" 4)"
  if [ ! -f "$f" ]; then echo "  ✗ 包里缺少 $s 的源码（$(basename "$f")）"; bad=1; continue; fi
  [ "$(sha256_of "$f")" = "$(lock_field "$s" 5)" ] || { echo "  ✗ $(basename "$f") 的 sha256 与 versions.lock 不符"; bad=1; }
done
n_files=$(find "$root/sources" -type f | wc -l | tr -d ' ')
[ "$n_files" -eq "$n_used" ] || { echo "  ✗ sources/ 里有 $n_files 个文件，这次构建用到 $n_used 份"; bad=1; }
[ $bad -eq 0 ] && echo "  ✓ 源码包：${n_used} 份上游源码齐全、sha256 全对；脚本、补丁、版本锁、工作流都在"

# 缓存键里有工作目录的绝对路径：两边用同一个
export LIBMPV_WORK="${LIBMPV_WORK:-$WORK}"
for t in $IOS_TARGETS; do
  repo_keys=$("$here/build.sh" --keys --arch "$t")
  bundle_keys=$(LIBMPV_SOURCES="$root/sources" "$root/scripts/ios/libmpv/build.sh" --keys --arch "$t")
  if [ -n "$repo_keys" ] && [ "$repo_keys" = "$bundle_keys" ]; then
    echo "  ✓ 包里的脚本算出的缓存键与仓库一致（${t}，$(wc -l <<<"$repo_keys" | tr -d ' ') 个组件）"
  else
    echo "  ✗ 包里的脚本算出的缓存键与仓库不同（${t}）："
    diff <(echo "$repo_keys") <(echo "$bundle_keys") | sed 's/^/      /' || true
    bad=1
  fi
done
[ $bad -eq 0 ] || die "对应源码包不完整"
