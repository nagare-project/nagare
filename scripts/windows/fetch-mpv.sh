#!/usr/bin/env bash
# 按 scripts/windows/mpv.lock 下载、校验并解压 shinchiro 的 mpv Windows 构建：
#   dist/mpv-win/       解压后的运行时（mpv.exe、mpv.com、d3dcompiler_43.dll、mpv/、doc/）
#   dist/mpv-sources/   mpv-winbuild-cmake 对应 commit 的源码快照（随 Release 上传，GPL 合规）
# 由 .goreleaser.yaml 里 windows build 的 pre hook 调用（before hook 阶段 dist/ 必须保持为空）。缓存目录默认 dist/cache/（注意 goreleaser --clean
# 会连它一起清掉；本机反复构建可 export NAGARE_MPV_CACHE_DIR=~/.cache/nagare-build）。
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
lock="$root/scripts/windows/mpv.lock"
cache="${NAGARE_MPV_CACHE_DIR:-$root/dist/cache}"
dest="$root/dist/mpv-win"
sources_dir="$root/dist/mpv-sources"

lock_get() {
  local v
  v=$(grep -E "^$1=" "$lock" | head -1 | cut -d= -f2-)
  [ -n "$v" ] || { echo "mpv.lock 缺少字段：$1" >&2; exit 1; }
  printf '%s' "$v"
}

sha256_of() {
  if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

# fetch <url> <目标文件> <期望 sha256>：已存在且哈希匹配则跳过；下载后哈希不符立即失败。
fetch() {
  local url=$1 file=$2 want=$3 got
  if [ -f "$file" ] && [ "$(sha256_of "$file")" = "$want" ]; then
    echo "==> 缓存命中：$(basename "$file")"
    return
  fi
  echo "==> 下载：$url"
  curl -fL --retry 3 --retry-delay 2 -o "$file.part" "$url"
  got=$(sha256_of "$file.part")
  if [ "$got" != "$want" ]; then
    rm -f "$file.part"
    echo "sha256 校验失败：$(basename "$file")" >&2
    echo "  期望 $want" >&2
    echo "  实际 $got" >&2
    echo "  若上游确实换了文件，请按 mpv.lock 顶部的步骤重新钉死。" >&2
    exit 1
  fi
  mv "$file.part" "$file"
}

sevenzip=""
for c in 7zz 7z; do command -v "$c" >/dev/null && { sevenzip=$c; break; }; done
[ -n "$sevenzip" ] || { echo "缺少 7zz（brew install sevenzip / apt install 7zip）" >&2; exit 1; }

asset=$(lock_get asset)
commit=$(lock_get commit)
mkdir -p "$cache" "$sources_dir"

fetch "$(lock_get url)" "$cache/$asset" "$(lock_get sha256)"

echo "==> 解压到 $dest"
rm -rf "$dest"
mkdir -p "$dest"
"$sevenzip" x -y -o"$dest" "$cache/$asset" >/dev/null
# 去掉与 nagare 无关的东西：文件关联安装器、自更新脚本。nagare 只需要能被拉起的 mpv。
rm -rf "$dest/installer" "$dest/updater.bat" "$dest/mpv-register.bat" "$dest/mpv-unregister.bat"
for need in mpv.exe mpv.com d3dcompiler_43.dll mpv/fonts.conf; do
  [ -e "$dest/$need" ] || { echo "解压后缺少 $need，上游布局变了？" >&2; exit 1; }
done

sources_file="$sources_dir/mpv-winbuild-cmake-$commit.tar.gz"
fetch "$(lock_get sources_url)" "$cache/$(basename "$sources_file")" "$(lock_get sources_sha256)"
cp "$cache/$(basename "$sources_file")" "$sources_file"

echo "==> mpv $(lock_get release_tag)（git $(lock_get mpv_git)）就绪："
ls -la "$dest"
