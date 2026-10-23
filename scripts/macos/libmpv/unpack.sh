#!/usr/bin/env bash
# 发布流水线用：把同一次运行里 libmpv-macos.yml 构建并验证过的两个 artifact 解开、再核对一遍，
# 交给 goreleaser 的 darwin-desktop 构建（链接）与 scripts/macos/build-app.sh（打进 Nagare.app）。
#
# 用法：unpack.sh <二进制 artifact 目录> <源码 artifact 目录> <解到哪>
#   两个目录里各应只有一个 libmpv-macos-universal-<id>.tar.gz / libmpv-macos-source-<id>.tar.gz，
#   <id> = <mpv 版本>-<提交前 10 位>，两边必须相同。
# 成功时在标准输出打两行（可直接追加进 ${GITHUB_ENV}）：
#   LIBMPV_DIR=<解开的 libmpv-macos-universal 目录>
#   LIBMPV_SOURCE=<源码包路径>
# 任何一步不对就失败：发布宁可停下，也不带着一套没核对过的 libmpv 出去。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
die() { echo "unpack.sh: $*" >&2; exit 1; }
[ $# -eq 3 ] || die "用法：unpack.sh <二进制 artifact 目录> <源码 artifact 目录> <解到哪>"
bin_dir=$1 src_dir=$2 dest=$3

# 恰好一个匹配的文件，否则失败
only() {
  local dir=$1 pattern=$2 found=()
  shopt -s nullglob
  found=("$dir"/$pattern)
  shopt -u nullglob
  [ "${#found[@]}" -eq 1 ] || die "$dir 里应恰好有一个 ${pattern}，实际 ${#found[@]} 个"
  [ -f "${found[0]}" ] && [ ! -L "${found[0]}" ] || die "${found[0]} 不是普通文件"
  printf '%s\n' "${found[0]}"
}
bin=$(only "$bin_dir" 'libmpv-macos-universal-*.tar.gz')
src=$(only "$src_dir" 'libmpv-macos-source-*.tar.gz')
id=$(basename "$bin" .tar.gz); id=${id#libmpv-macos-universal-}
src_id=$(basename "$src" .tar.gz); src_id=${src_id#libmpv-macos-source-}
[ "$id" = "$src_id" ] || die "二进制（${id}）与源码包（${src_id}）不是同一次构建"
[[ "$id" =~ ^[0-9]+\.[0-9]+\.[0-9]+-[0-9a-f]{10}$ ]] || die "构建号形状不对：$id"

# 解压前查条目：只能是 libmpv-macos-universal/ 下的普通文件与目录（下载的东西一律当不可信数据）
listing=$(tar -tvzf "$bin") || die "读不了 $bin"
if grep -Ev '^[-d]' <<<"$listing" | grep -q .; then
  die "归档里有普通文件与目录以外的条目（符号链接、硬链接、设备……），拒绝解压"
fi
while IFS= read -r name; do
  case "$name" in
    libmpv-macos-universal/* | libmpv-macos-universal) ;;
    *) die "归档里有 libmpv-macos-universal/ 之外的条目：$name" ;;
  esac
  case "/$name/" in */../*) die "归档条目带 ..：$name" ;; esac
done < <(tar -tzf "$bin")

# 对应源码包也要真是那一套：能读、顶层就是 libmpv-macos-source-<id>/，里面有 mpv 的源码归档与我们的构建脚本、
# versions.lock。只核对文件名的话，一个空的或截断的包照样会挂上 Release —— 那就是在不合规地分发。
src_listing=$(tar -tzf "$src") || die "读不了源码包 ${src}（截断了？）"
src_top="libmpv-macos-source-${id}"
if grep -v -e "^${src_top}/" -e "^${src_top}\$" <<<"$src_listing" | grep -q .; then
  die "源码包里有 ${src_top}/ 之外的条目"
fi
for need in "${src_top}/scripts/macos/libmpv/versions.lock" "${src_top}/scripts/macos/libmpv/build.sh" "${src_top}/README.txt"; do
  grep -qxF "$need" <<<"$src_listing" || die "源码包里缺少 ${need}"
done
grep -qE "^${src_top}/sources/mpv-[0-9.]+\.tar\.gz\$" <<<"$src_listing" || die "源码包里没有 mpv 的源码归档"

mkdir -p "$dest"
dest=$(cd "$dest" && pwd)
out="$dest/libmpv-macos-universal"
rm -rf "$out"
tar -xzf "$bin" -C "$dest"
[ -d "$out" ] || die "解开后没有 libmpv-macos-universal/"

echo "==> 按清单核对 $(basename "$bin")" >&2
python3 -I -B "$here/tools/macho.py" verify-manifest "$out" >&2 || die "与清单不符"
echo "==> Mach-O 静态检查" >&2
python3 -I -B "$here/tools/macho.py" check "$out" --minos 13.0 >&2 || die "Mach-O 静态检查不过"
for f in licenses manifest.json MANIFEST.txt VERIFY.txt; do
  [ -e "$out/$f" ] || die "缺少 $f"
done

# 清单里记的构建来源要与文件名对得上（同一个提交、同一个 mpv 版本）
commit=$(plutil -extract source_commit raw -o - "$out/manifest.json")
mpv_ver=$(plutil -extract components.0.version raw -o - "$out/manifest.json")
mpv_name=$(plutil -extract components.0.name raw -o - "$out/manifest.json")
[ "$mpv_name" = mpv ] || die "清单的第一个组件不是 mpv（是 ${mpv_name}）"
[ "${mpv_ver}-${commit:0:10}" = "$id" ] || die "清单（${mpv_ver}-${commit:0:10}）与文件名（${id}）对不上"

echo "LIBMPV_DIR=$out"
echo "LIBMPV_SOURCE=$(cd "$(dirname "$src")" && pwd)/$(basename "$src")"
