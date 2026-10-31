#!/usr/bin/env bash
# internal/api 的依赖图守卫。
#
# 磁力引擎、来源插件、磁力源规则只能经 internal/api/sources 进二进制 —— 那个子包只由桌面版的 main 注册。
# internal/api 一旦（直接或间接）依赖它们，不 import api/sources 的构建（iOS）也会把整套磁力代码编进去，
# 而且只有那次构建才发现得了。这里每个目标平台各查一次：平台专属文件（_windows.go 之类）可能各自引入依赖；
# darwin / ios 另按 cgo + libmpv 构建标签再查一次（带 libmpv 的构建会编进另一批文件）。
#
# 用法：scripts/ci/check-api-deps.sh [包...]   不给包时查 ./internal/api
set -euo pipefail

cd "$(dirname "$0")/../.."

if [ $# -eq 0 ]; then
	set -- ./internal/api
fi

# 一行一个导入路径。用 [.] 而不是 \.：同一个正则还要交给 awk，awk -v 会吃掉反斜杠。
forbidden='^(github[.]com/nagare-project/nagare/internal/(torrentstream|sourceplugin|rules|rulesync)|github[.]com/anacrolix/[^ ]+)(/[^ ]*)?$'
# 目标写成 goos/goarch，带 :libmpv 的按 CGO_ENABLED=1 -tags libmpv 查（go list 只解析，不调 C 编译器）。
targets="linux/amd64 darwin/arm64 windows/amd64 ios/arm64 darwin/arm64:libmpv ios/arm64:libmpv"

list_deps() { # list_deps <目标> <go list 的其余参数...>
	local target=$1 platform cgo=0 tags=""
	shift
	platform=${target%%:*}
	if [ "$platform" != "$target" ]; then
		cgo=1
		tags=${target#*:}
	fi
	GOOS=${platform%/*} GOARCH=${platform#*/} CGO_ENABLED=$cgo go list -tags "$tags" -deps "$@"
}

# 自检：api/sources 本来就依赖这些包。在那里都查不出来，说明正则或 go list 的用法错了 ——
# 那样守卫会永远是绿的，比没有守卫更糟。先把输出存下来再 grep：管道里 grep -q 提前退出会让上游
# 收到 SIGPIPE，在 pipefail 下被当成失败。
self=$(list_deps linux/amd64 ./internal/api/sources)
if ! grep -Eq "$forbidden" <<<"$self"; then
	echo "::error::依赖图守卫自检失败：internal/api/sources 里没查到磁力相关依赖，检查本脚本的正则"
	exit 1
fi

status=0
for target in $targets; do
	# go list 自己失败（包名写错、构建约束把包整个排除）不能当成「没查到」放过去
	if ! deps=$(list_deps "$target" "$@"); then
		echo "::error::go list 失败（${target}），没法检查 ${*} 的依赖图"
		status=1
		continue
	fi
	hits=$(grep -E "$forbidden" <<<"$deps" || true)
	if [ -z "$hits" ]; then
		echo "✓ ${*}（${target}）不依赖磁力 / 来源插件 / 规则引擎"
		continue
	fi
	status=1
	echo "::error::${*}（${target}）依赖了只该经 internal/api/sources 进来的包："
	echo "$hits" | sed 's/^/    /'
	# 指出是谁直接 import 的：上面那份清单里有传递依赖，修的时候要找的是这一层
	echo "  直接引入者："
	list_deps "$target" -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}' "$@" |
		awk -v re="$forbidden" '$1 !~ re { for (i = 2; i <= NF; i++) if ($i ~ re) print "    " $1 " → " $i }'
done

if [ "$status" -ne 0 ]; then
	echo "磁力、来源插件、规则引擎相关的代码放进 internal/api/sources（只由桌面版 main 注册），internal/api 只留共用部分。" >&2
fi
exit "$status"
