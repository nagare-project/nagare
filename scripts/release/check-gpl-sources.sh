#!/usr/bin/env bash
# 校验发布产物里带着 GPL 要求随二进制提供的对应源码，而且对得上这一版实际打包的二进制。
#
#   Windows  安装包 / 便携 zip 里的 mpv（shinchiro 构建）→ mpv-winbuild-cmake-<提交>.tar.gz
#            （scripts/windows/fetch-mpv.sh 按 mpv.lock 放进 dist/mpv-sources/）
#   macOS    Nagare.app 里进程内链接的 libmpv 与命令行 mpv（nagare 自建）→ libmpv-macos-source-<构建号>.tar.gz
#            （scripts/macos/build-app.sh 放进 dist/mpv-sources/），构建号必须与 .app 里的
#            Contents/Resources/libmpv/manifest.json 是同一次构建
#
# 两个文件都要出现在 checksums.txt 里：goreleaser 的 checksum 与 release 的 extra_files 用的是同一个
# glob（dist/mpv-sources/*），进了校验清单就会挂上 Release。少了哪个，那个平台的包就是在不合规地分发。
#
# 用法：scripts/release/check-gpl-sources.sh [校验清单路径 | dist 目录] [Nagare.app 路径]
#   给 dist 目录时核对的是 <dist>/mpv-sources/ 里的文件：goreleaser 的 hook 在上传草稿之前这样调
#   （那时还没有 checksums.txt）；release.yml 在 goreleaser 之后再按 checksums.txt 核一遍。
set -euo pipefail

manifest=${1:-dist/checksums.txt}
app=${2:-dist/Nagare.app}
root=$(cd "$(dirname "$0")/../.." && pwd)
missing=0
# names：要核对的文件名，每行一个（checksums.txt 的第二列，或 dist/mpv-sources/ 的文件）
if [ -d "$manifest" ]; then
  names=$(ls -1 "$manifest/mpv-sources" 2>/dev/null || true)
  where="${manifest}/mpv-sources/"
elif [ -f "$manifest" ]; then
  names=$(awk '{print $NF}' "$manifest")
  where="校验清单"
else
  echo "找不到校验清单或 dist 目录：$manifest" >&2
  exit 1
fi
has() { grep -qxF -- "$1" <<<"$names"; }

# Windows：快照名里的提交来自 mpv.lock（与 fetch-mpv.sh 同一个字段）
win_commit=$(grep -E '^commit=' "$root/scripts/windows/mpv.lock" | head -1 | cut -d= -f2-)
if [ -z "$win_commit" ]; then
  echo "  ✗ scripts/windows/mpv.lock 里读不到 commit" >&2
  missing=1
elif has "mpv-winbuild-cmake-${win_commit}.tar.gz"; then
  echo "  ✓ Windows mpv：mpv-winbuild-cmake-${win_commit}.tar.gz"
else
  echo "  ✗ ${where}里没有 mpv-winbuild-cmake-${win_commit}.tar.gz" >&2
  missing=1
fi

# macOS：源码包的构建号 = <mpv 版本>-<构建提交前 10 位>，从 .app 里实际打包的那份清单算
lib_manifest="$app/Contents/Resources/libmpv/manifest.json"
if [ ! -f "$lib_manifest" ]; then
  echo "  ✗ 找不到 ${lib_manifest}（Nagare.app 里没有 libmpv 的构建清单）" >&2
  missing=1
else
  commit=$(plutil -extract source_commit raw -o - "$lib_manifest")
  mpv_ver=$(plutil -extract components.0.version raw -o - "$lib_manifest")
  want="libmpv-macos-source-${mpv_ver}-${commit:0:10}.tar.gz"
  if has "$want"; then
    echo "  ✓ macOS libmpv：${want}"
  else
    echo "  ✗ ${where}里没有 ${want}（Nagare.app 里的 libmpv 是这次构建的）" >&2
    missing=1
  fi
fi

if [ "$missing" -ne 0 ]; then
  echo "对应源码不全：这一版的二进制不能这样发布（GPL）。见 THIRD_PARTY_NOTICES.md 与 docs/releasing.md。" >&2
  exit 1
fi
echo "==> mpv 的对应源码齐了"
