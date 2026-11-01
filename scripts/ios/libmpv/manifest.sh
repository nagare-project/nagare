#!/usr/bin/env bash
# 给 package.sh xcframework 的产物写清单（manifest.json / MANIFEST.txt）：组件版本、选用的许可证、
# 源码 sha256、每个切片的架构与 minos、每个文件的 sha256。verify.sh 通过之后再跑（CI 先把 VERIFY.txt 放进去）。
#
# 用法：manifest.sh <产物目录>
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

[ $# -eq 1 ] || die "用法：manifest.sh <产物目录>"
used=$(mktemp)
trap 'rm -f "$used"' EXIT
ios_used_sources > "$used"
python3 -I -B "$here/tools/xcf.py" manifest "$1" --lock "$LOCK_FILE" --choices "$here/license-choices.txt" \
  --used "$used" --version "$(lock_field mpv 2)" --minos "$DEPLOYMENT_TARGET"
