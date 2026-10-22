# shellcheck shell=bash
# versions.lock 的读取、下载、校验与解包。依赖 lib/env.sh。

LOCK_FILE="$LIBMPV_DIR/versions.lock"

# 列出 lock 里的全部源码名
lock_names() {
  awk -F'|' '!/^[[:space:]]*(#|$)/ { gsub(/[[:space:]]/, "", $1); print $1 }' "$LOCK_FILE"
}

# lock_field <名字> <列号>：1 名字 2 版本 3 许可证 4 文件名 5 sha256 6 地址
lock_field() {
  awk -F'|' -v n="$1" -v c="$2" '
    !/^[[:space:]]*(#|$)/ {
      name = $1; gsub(/[[:space:]]/, "", name)
      if (name == n) { v = $c; gsub(/^[[:space:]]+|[[:space:]]+$/, "", v); print v; found = 1; exit }
    }
    END { if (!found) exit 1 }' "$LOCK_FILE" || die "versions.lock 里没有 $1"
}

# 规范化后的整行（进缓存键；列之间的空白不影响键）
lock_line() {
  local i out=""
  for i in 1 2 3 4 5 6; do out="$out$(lock_field "$1" "$i")|"; done
  printf '%s\n' "$out"
}

source_archive() { echo "$SOURCES_DIR/$(lock_field "$1" 4)"; }

sha256_of() { shasum -a 256 "$1" | awk '{print $1}'; }

# 下载并校验一份源码；已有且哈希相符就不再下载
fetch_source() {
  local name=$1 file url sha dest tmp actual
  file=$(lock_field "$name" 4)
  sha=$(lock_field "$name" 5)
  url=$(lock_field "$name" 6)
  dest="$SOURCES_DIR/$file"
  mkdir -p "$SOURCES_DIR"
  if [ -f "$dest" ] && [ "$(sha256_of "$dest")" = "$sha" ]; then
    return 0
  fi
  log "下载 ${name}：$url"
  tmp="$dest.part"
  rm -f "$tmp"
  curl -fsSL --retry 5 --retry-delay 5 --connect-timeout 30 -o "$tmp" "$url" || die "下载失败：$url"
  actual=$(sha256_of "$tmp")
  if [ "$actual" != "$sha" ]; then
    rm -f "$tmp"
    die "$file 的 sha256 不符：期望 ${sha}，实际 $actual"
  fi
  mv "$tmp" "$dest"
}

# extract_source <名字> <目标目录>：解开并去掉归档里唯一的顶层目录
extract_source() {
  local name=$1 dest=$2 archive
  archive=$(source_archive "$name")
  [ -f "$archive" ] || die "源码未下载：${archive}（先跑 fetch.sh）"
  [ "$(sha256_of "$archive")" = "$(lock_field "$name" 5)" ] || die "$archive 的 sha256 不符（下载后被改动过？）"
  rm -rf "$dest"
  mkdir -p "$dest"
  tar -xf "$archive" -C "$dest" --strip-components=1
}
