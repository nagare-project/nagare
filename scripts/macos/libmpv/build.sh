#!/usr/bin/env bash
# 为一个架构构建 libmpv 及其全部依赖，装进 $WORK/prefix/<架构>。
#
# 用法：
#   build.sh [--arch arm64|x86_64] [组件...]    构建（默认本机架构、全部组件）
#   build.sh --keys [--arch ...]                只打印每个组件的缓存键
#
# 每个组件单独缓存：装到 $WORK/stage/<架构>/<组件>/ 并写下它的键（源码 + 补丁 + recipe 函数体 +
# 工具链 + 上游组件的键 的 sha256）。键没变就直接复用，变了只重编它和依赖它的组件。
# prefix 每次从各组件的 stage 重新拼出来，所以删掉/改名的组件不会留下旧文件。
#
# 在 CI 里（设了 GITHUB_OUTPUT）会输出 changed=true|false：有组件重编过才需要保存缓存。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib/env.sh
. "$here/lib/env.sh"
# shellcheck source=lib/sources.sh
. "$here/lib/sources.sh"
# shellcheck source=lib/recipes.sh
. "$here/lib/recipes.sh"

ARCH=$HOST_ARCH
mode=build
wanted=""
while [ $# -gt 0 ]; do
  case "$1" in
    --arch) ARCH=$2; shift 2 ;;
    --keys) mode=keys; shift ;;
    -h | --help) sed -n '2,13p' "$0"; exit 0 ;;
    -*) die "未知参数：$1" ;;
    *) wanted="$wanted $1"; shift ;;
  esac
done
check_arch "$ARCH"
[ -n "$wanted" ] || wanted="mpv"

require_tools xcrun clang meson ninja cmake nasm pkg-config autoreconf glibtoolize make shasum tar curl

dep_var() { eval "printf '%s' \"\${${1}_${2}:-}\""; }

is_dep() { case " $DEP_ORDER " in *" $1 "*) return 0 ;; esac; return 1; }

# 组件闭包（按 DEP_ORDER 排好）
closure() {
  local todo=$1 seen="" d changed=1
  while [ $changed = 1 ]; do
    changed=0
    for d in $todo; do
      is_dep "$d" || die "未知组件：${d}（可选：${DEP_ORDER}）"
      case " $seen " in *" $d "*) continue ;; esac
      seen="$seen $d"
      todo="$todo $(dep_var "$d" deps)"
      changed=1
    done
  done
  for d in $DEP_ORDER; do
    case " $seen " in *" $d "*) printf '%s ' "$d" ;; esac
  done
}

# 缓存键。按 DEP_ORDER（已是拓扑序）依次算，结果放在 KEY_<组件> 里；
# 工具链指纹与脚本哈希先算好，避免在子 shell 里反复调用 xcodebuild。
# 键里带 lib/env.sh（编译助手）但不带本文件：改了 build_dep 装出来的内容（stage 的布局、
# 许可证的位置……）时，把下面的 KEY_FORMAT 加一，强制全部重编。
KEY_FORMAT=1
compute_keys() {
  local fp helpers dep key s d p
  fp=$(toolchain_fingerprint)
  helpers=$(sha256_of "$here/lib/env.sh")
  for dep in $DEP_ORDER; do
    key=$(
      {
        echo "nagare-libmpv-key $KEY_FORMAT"
        echo "arch $ARCH"
        echo "prefix $(prefix_dir "$ARCH")"
        printf '%s\n' "$fp"
        echo "helpers $helpers"
        for s in $(dep_var "$dep" sources); do lock_line "$s"; done
        echo "licenses $(dep_var "$dep" licenses)"
        declare -f "recipe_$dep"
        if [ -d "$here/patches/$dep" ]; then
          for p in "$here/patches/$dep"/*.patch; do
            [ -f "$p" ] && echo "patch $(basename "$p") $(sha256_of "$p")"
          done
        fi
        for d in $(dep_var "$dep" deps); do echo "dep $d $(dep_key "$d")"; done
      } | shasum -a 256 | awk '{print $1}'
    )
    eval "KEY_${dep}=$key"
  done
}

dep_key() { eval "printf '%s\n' \"\${KEY_${1}:?缓存键未计算：$1}\""; }

apply_patches() {
  local dep=$1 dir=$2 p
  [ -d "$here/patches/$dep" ] || return 0
  for p in "$here/patches/$dep"/*.patch; do
    [ -f "$p" ] || continue
    log "${dep}：打补丁 $(basename "$p")"
    patch -d "$dir" -p1 -N --batch -i "$p" >/dev/null || die "${dep}：补丁 $(basename "$p") 打不上"
  done
}

build_dep() {
  local dep=$1 key=$2 stage
  stage="$(stage_root "$ARCH")/$dep"
  SRC="$WORK/src/$ARCH/$dep"
  OBJ="$WORK/obj/$ARCH/$dep"
  DEST="$WORK/dest/$ARCH/$dep"
  rm -rf "$SRC" "$OBJ" "$DEST" "$stage"
  mkdir -p "$OBJ" "$DEST"

  local first
  # shellcheck disable=SC2046 # 故意按空白拆开（清单可以跨行）
  first=$(printf '%s\n' $(dep_var "$dep" sources) | head -1)
  extract_source "$first" "$SRC"
  apply_patches "$dep" "$SRC"

  log "${dep}（${ARCH}）：开始编译"
  local t0=$SECONDS
  "recipe_$dep"

  local inst="$DEST$PREFIX"
  [ -d "$inst" ] || die "$dep 没有装出任何文件（期望在 ${inst}）"
  # libtool 的 .la、文档与 man 都用不上；.la 里还写着构建机路径
  find "$inst" -name '*.la' -delete
  rm -rf "$inst/share/man" "$inst/share/doc" "$inst/share/info" "$inst/share/gtk-doc"
  local lic
  mkdir -p "$inst/share/licenses/$dep"
  for lic in $(dep_var "$dep" licenses); do
    [ -f "$SRC/$lic" ] || die "${dep}：许可证文件不存在：$lic"
    cp "$SRC/$lic" "$inst/share/licenses/$dep/"
  done

  mkdir -p "$(dirname "$stage")"
  mv "$inst" "$stage"
  echo "$key" > "$stage/.nagare-key"
  rm -rf "$DEST" "$SRC"
  log "${dep}（${ARCH}）：完成，用时 $((SECONDS - t0)) 秒"
}

deps=$(closure "$wanted")
PREFIX=$(prefix_dir "$ARCH")
compute_keys

if [ "$mode" = keys ]; then
  for dep in $deps; do echo "$dep $(dep_key "$dep")"; done
  exit 0
fi

log "架构 ${ARCH}，组件：$deps"
log "工具链：$(toolchain_fingerprint | tr '\n' ';')"
for dep in $deps; do
  for s in $(dep_var "$dep" sources); do fetch_source "$s"; done
done

rm -rf "$PREFIX"
mkdir -p "$PREFIX/lib/pkgconfig" "$PREFIX/include"
changed=false
# 中途失败也要报告：前面已经编好的组件值得进缓存，下次从失败的那个接着编
report_changed() { if [ -n "${GITHUB_OUTPUT:-}" ]; then echo "changed=$changed" >> "$GITHUB_OUTPUT"; fi; }
trap report_changed EXIT
for dep in $deps; do
  key=$(dep_key "$dep")
  stage="$(stage_root "$ARCH")/$dep"
  if [ -f "$stage/.nagare-key" ] && [ "$(cat "$stage/.nagare-key")" = "$key" ]; then
    log "${dep}（${ARCH}）：缓存命中 ${key:0:12}"
  else
    build_dep "$dep" "$key"
    changed=true
  fi
  # 拼进 prefix，供后面的组件编译时找到（保留符号链接）
  (cd "$stage" && tar -cf - --exclude .nagare-key .) | (cd "$PREFIX" && tar -xf -)
done

# 清掉 stage 里已经不存在的组件（缓存里带过来的、recipes.sh 删掉了的）
for d in "$(stage_root "$ARCH")"/*; do
  [ -d "$d" ] || continue
  is_dep "$(basename "$d")" && continue
  log "移除不再使用的 stage：$(basename "$d")"
  rm -rf "$d"
  changed=true
done

log "完成：${PREFIX}（有组件重编：${changed}）"
