#!/usr/bin/env bash
# 按 scripts/release/source-plugin.lock 下载、校验并解压 Nagare Source 插件归档：
#   dist/source-plugin/<OS>_<arch>/nagare-source[.exe]   引擎
#   dist/source-plugin/<OS>_<arch>/repo/                   只含 BT 规则的运行时根目录
# 用法：fetch-source-plugin.sh <goos> <goarch>   （darwin 会同时取 arm64 与 amd64，universal .app 两个都要）
# 由 .goreleaser.yaml 各 build 的 pre hook 调用；缓存目录默认 dist/cache/。
# lock 里 version 为空时只写一个说明文件并成功返回（安装包不捆插件）；sha256 不符立即失败。
set -euo pipefail

goos=${1:?goos}; goarch=${2:?goarch}
root=$(cd "$(dirname "$0")/../.." && pwd)
lock="$root/scripts/release/source-plugin.lock"
cache="${NAGARE_PLUGIN_CACHE_DIR:-$root/dist/cache}"
base="$root/dist/source-plugin"

lock_get() { grep -E "^$1=" "$lock" | head -1 | cut -d= -f2-; }
sha256_of() { if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }
os_name() { case "$1" in darwin) echo MacOS;; linux) echo Linux;; windows) echo Windows;; *) echo "未知 GOOS：$1" >&2; exit 1;; esac; }
arch_name() { case "$1" in amd64) echo x86_64;; arm64) echo arm64;; *) echo "未知 GOARCH：$1" >&2; exit 1;; esac; }

version=$(lock_get version)
archs=("$goarch"); [ "$goos" = darwin ] && archs=(arm64 amd64)

# goreleaser 对同一个 build 的每个目标各跑一次 pre hook，而且是并发的：darwin 的两个
# arch 会同时进来取同一批归档。用 mkdir 当互斥锁串行化（macOS 没有 flock），
# 后进来的那个拿到锁时缓存已命中、目录已就位，只是再校验一遍。
mkdir -p "$root/dist"
lockdir="$root/dist/.source-plugin.lock"
for _ in $(seq 1 600); do mkdir "$lockdir" 2>/dev/null && break; sleep 1; done
[ -d "$lockdir" ] || { echo "等待 fetch-source-plugin 锁超时" >&2; exit 1; }
trap 'rmdir "$lockdir" 2>/dev/null' EXIT

for arch in "${archs[@]}"; do
  os=$(os_name "$goos"); ar=$(arch_name "$arch")
  dest="$base/${os}_${ar}"
  rm -rf "$dest"; mkdir -p "$dest" "$cache"
  if [ -z "$version" ]; then
    echo "::warning::source-plugin.lock 未钉版本，本次 ${os}_${ar} 构建不捆绑 Nagare Source 插件"
    printf '本次构建未捆绑 Nagare Source 插件。\n可从 https://github.com/nagare-project/Nagare_Source/releases 自行安装，然后在设置 › 来源里指定路径。\n' > "$dest/README.txt"
    continue
  fi
  want=$(lock_get "sha256_${os}_${ar}")
  [ -n "$want" ] || { echo "source-plugin.lock 缺少 sha256_${os}_${ar}" >&2; exit 1; }
  ext=tar.gz; [ "$goos" = windows ] && ext=zip
  asset="nagare-source-${version}_${os}_${ar}.${ext}"
  # NAGARE_PLUGIN_ASSET_BASE 可指向镜像或本机目录（file://），用于流水线自测与备用分发源
  url="${NAGARE_PLUGIN_ASSET_BASE:-https://github.com/nagare-project/Nagare_Source/releases/download/v${version}}/${asset}"
  file="$cache/$asset"
  if [ ! -f "$file" ] || [ "$(sha256_of "$file")" != "$want" ]; then
    echo "==> 下载：$url"
    part="$file.part.$$"
    curl -fL --retry 3 --retry-delay 2 -o "$part" "$url"
    got=$(sha256_of "$part")
    if [ "$got" != "$want" ]; then
      rm -f "$part"
      echo "sha256 校验失败：$asset（期望 $want，实际 $got）" >&2
      exit 1
    fi
    mv "$part" "$file"
  else
    echo "==> 缓存命中：$asset"
  fi
  echo "==> 解压到 $dest"
  tmp=$(mktemp -d)
  if [ "$ext" = zip ]; then unzip -q "$file" -d "$tmp"; else tar -xzf "$file" -C "$tmp"; fi
  inner=$(find "$tmp" -mindepth 1 -maxdepth 1 -type d | head -1)
  [ -n "$inner" ] || { echo "归档结构异常：$asset" >&2; exit 1; }
  cp -R "$inner"/. "$dest"/
  rm -rf "$tmp"
  # 归档里只允许出现引擎、BT 规则根目录和许可证；任何在线规则混进来都是流水线事故
  if [ -d "$dest/repo/sources/web" ] || [ -d "$dest/repo/sources/upstreams" ]; then
    echo "插件归档含在线规则（sources/web 或 sources/upstreams），拒绝捆绑" >&2; exit 1
  fi
  bin="$dest/nagare-source"; [ "$goos" = windows ] && bin="$dest/nagare-source.exe"
  [ -f "$bin" ] && [ -f "$dest/repo/schema/source-v1.schema.json" ] || { echo "归档缺少引擎或 repo：$asset" >&2; exit 1; }
  chmod 755 "$bin"
done

# macOS 的 universal 二进制两种 arch 都可能跑：合成一个目录，两个引擎按 GOARCH 命名，
# repo 共用一份。归档与 .app 都从这里取（dist/source-plugin/MacOS_universal）。
if [ "$goos" = darwin ]; then
  uni="$base/MacOS_universal"
  rm -rf "$uni"; mkdir -p "$uni"
  if [ -z "$version" ]; then
    cp "$base/MacOS_arm64/README.txt" "$uni/README.txt"
  else
    cp "$base/MacOS_arm64/nagare-source" "$uni/nagare-source-arm64"
    cp "$base/MacOS_x86_64/nagare-source" "$uni/nagare-source-amd64"
    cp -R "$base/MacOS_arm64/repo" "$uni/repo"
    cp "$base/MacOS_arm64/LICENSE" "$uni/LICENSE"
  fi
fi
