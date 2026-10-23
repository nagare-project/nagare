#!/usr/bin/env bash
# 用客户端自更新【同一套】Go 解包代码验发布出去的 .app.zip，并在这台机器的架构上真跑一遍包里的 nagare 与 mpv
# （internal/selfupdate 的 TestReleaseAppZip）。CI 的 runner 是 arm64，我们手上没有 Apple Silicon 的 Mac ——
# arm64 切片能不能把 Contents/Frameworks 里的库全部加载起来、解得了片，只在这里看得到。
#
# 由 .goreleaser.yaml 的 darwin-desktop-universal post hook 在 build-app.sh 之后调用：放在 goreleaser 里而不是
# release.yml 的后续步骤，是为了它不过时 goreleaser 当场失败，打 tag 那次不会先把草稿 Release 和产物传上去。
#
# 用法：scripts/release/check-app-zip.sh <nagare-<版本>_MacOS_universal.app.zip>
set -euo pipefail

[ $# -eq 1 ] || { echo "用法：$0 <.app.zip>" >&2; exit 2; }
[ -f "$1" ] || { echo "找不到 .app.zip：$1" >&2; exit 1; }
zip="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
echo "==> 自更新解包往返 + 启动检查（$(uname -m)）：$(basename "$zip")"
NAGARE_RELEASE_APPZIP="$zip" go test ./internal/selfupdate -run '^TestReleaseAppZip$' -count=1 -v
