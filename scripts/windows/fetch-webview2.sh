#!/usr/bin/env bash
# 按 scripts/windows/webview2.lock 下载并校验 WebView2 Evergreen Bootstrapper：
#   dist/webview2/MicrosoftEdgeWebview2Setup.exe   （build-installer.sh 把它打进 NSIS 安装包）
# 由 .goreleaser.yaml 里 windows build 的 pre hook 调用；缓存目录同 fetch-mpv.sh（NAGARE_MPV_CACHE_DIR）。
# 用法：scripts/windows/fetch-webview2.sh [输出目录]（默认 dist/webview2）
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
lock="$root/scripts/windows/webview2.lock"
cache="${NAGARE_MPV_CACHE_DIR:-$root/dist/cache}"
dest="${1:-$root/dist/webview2}"
name=MicrosoftEdgeWebview2Setup.exe

lock_get() {
  local v
  v=$(grep -E "^$1=" "$lock" | head -1 | cut -d= -f2-)
  [ -n "$v" ] || { echo "webview2.lock 缺少字段：$1" >&2; exit 1; }
  printf '%s' "$v"
}

sha256_of() {
  if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

want=$(lock_get sha256)
mkdir -p "$cache" "$dest"
file="$cache/webview2-$want.exe"
if [ -f "$file" ] && [ "$(sha256_of "$file")" = "$want" ]; then
  echo "==> 缓存命中：${name}"
else
  echo "==> 下载：$(lock_get url)"
  # -sS：不打进度条（见 fetch-mpv.sh 的说明，goreleaser 钩子日志里的 \r 触发过 panic）
  curl -fsSL --retry 3 --retry-delay 2 -o "$file.part" "$(lock_get url)"
  got=$(sha256_of "$file.part")
  if [ "$got" != "$want" ]; then
    rm -f "$file.part"
    echo "sha256 校验失败：${name}" >&2
    echo "  期望 $want" >&2
    echo "  实际 $got" >&2
    echo "  微软若换了文件，按 webview2.lock 顶部的步骤重新钉死。" >&2
    exit 1
  fi
  mv "$file.part" "$file"
fi
cp "$file" "$dest/$name"
echo "==> WebView2 bootstrapper 就绪：${dest}/${name}（$(lock_get size) 字节，钉于 $(lock_get fetched)）"
