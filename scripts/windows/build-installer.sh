#!/usr/bin/env bash
# 用 makensis 把 nagare.exe + 内置 mpv 打成按用户安装的 NSIS 安装包。
# 由 .goreleaser.yaml 的 windows build post hook 调用（fetch-mpv.sh 已先把 mpv 放到 dist/mpv-win）。
# 用法：scripts/windows/build-installer.sh <nagare.exe 路径> <版本> <输出目录>
# 产物：<输出目录>/nagare-<版本>_Windows_x86_64-setup.exe
set -euo pipefail

if [ $# -ne 3 ]; then
  echo "用法：$0 <nagare.exe 路径> <版本> <输出目录>" >&2
  exit 2
fi
exe=$1
version=$2
mkdir -p "$3"
out=$(cd "$3" && pwd)
root=$(cd "$(dirname "$0")/../.." && pwd)
mpv_dir="$root/dist/mpv-win"

command -v makensis >/dev/null || { echo "缺少 makensis（brew install makensis / apt install nsis）" >&2; exit 1; }
[ -f "$exe" ] || { echo "nagare.exe 不存在：$exe" >&2; exit 1; }
[ -f "$mpv_dir/mpv.exe" ] || { echo "缺少 $mpv_dir/mpv.exe，先跑 scripts/windows/fetch-mpv.sh" >&2; exit 1; }

# VIProductVersion 只认 a.b.c.d 四段数字：取版本号开头的 a.b.c，snapshot 的 -next 之类后缀丢掉。
numeric=$(printf '%s' "$version" | grep -oE '^[0-9]+\.[0-9]+\.[0-9]+' || true)
[ -n "$numeric" ] || numeric="0.0.0"

stage=$(mktemp -d)
# trap 里要显式保住退出码：bash 3.2（macOS runner 自带的就是它）在 EXIT trap
# 跑完之后会用 trap 的退出码顶掉脚本自己的 —— 脚本失败了 CI 却是绿的。
trap 'ec=$?; rm -rf "$stage"; exit "$ec"' EXIT
cp "$exe" "$stage/nagare.exe"
# 许可证页用 RichEdit 显示，CRLF 才能正确换行
sed 's/$/\r/' "$root/LICENSE" > "$stage/LICENSE.txt"
cp "$root/THIRD_PARTY_NOTICES.md" "$stage/"
cp -R "$mpv_dir" "$stage/mpv"

outfile="$out/nagare-${version}_Windows_x86_64-setup.exe"
rm -f "$outfile"
echo "==> makensis → $outfile"
makensis -V2 -INPUTCHARSET UTF8 \
  -DVERSION="$version" \
  -DVERSION_NUMERIC="$numeric.0" \
  -DSRCDIR="$stage" \
  -DICON="$root/packaging/icon/nagare.ico" \
  -DOUTFILE="$outfile" \
  "$root/scripts/windows/nagare.nsi"
ls -la "$outfile"
