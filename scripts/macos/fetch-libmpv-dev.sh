#!/usr/bin/env bash
# ############################################################################################
# ##  仅供本机开发。绝不能随发布出去。 / LOCAL DEVELOPMENT ONLY. NEVER SHIP THESE DYLIBS.       ##
# ############################################################################################
#
# 下载 macOS 桌面窗口（internal/desktop，构建标签 libmpv）开发期要链接的 libmpv：
#   - IINA 的预编译依赖 iina-deps-fat.zip（iina/deps-buildscripts 的 iina-1.5.0-release，
#     universal、minos 11、mpv 0.41.0 + LuaJIT + libplacebo + GPL FFmpeg），sha256 钉死；
#   - mpv v0.41.0 的四个公开头文件（client.h / render.h / render_gl.h / stream_cb.h），sha256 钉死。
#
# 为什么不能发：iina/deps-buildscripts 仓库没有许可证文件，而 GPL 要求随二进制提供对应的构建
# 脚本 —— 用它的二进制发版做不到合规。发布版用我们自己构建的 libmpv（scripts/macos/libmpv/，
# 发布流水线现编现验）。这里的产物只放在仓库根的 .libmpv-dev/（已 gitignore），不进 dist/、
# 不进 .app、不进任何 Release（build-app.sh 只收按 manifest.json 核对得上的自建产物）。
#
# 用法：
#   scripts/macos/fetch-libmpv-dev.sh              下载、校验、解压到 .libmpv-dev/
#   scripts/macos/fetch-libmpv-dev.sh --print-env  只打印构建要用的 CGO_CFLAGS / CGO_LDFLAGS
# 构建见 scripts/macos/build-desktop-dev.sh 与 scripts/macos/libmpv-dev.md。
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
dest="$root/.libmpv-dev"

print_env() {
  echo "export CGO_CFLAGS=\"-I$dest/include\""
  echo "export CGO_LDFLAGS=\"-L$dest/lib -Wl,-rpath,$dest/lib\""
}
if [ "${1:-}" = "--print-env" ]; then
  print_env
  exit 0
fi

[ "$(uname -s)" = "Darwin" ] || { echo "只在 macOS 上用" >&2; exit 1; }

IINA_URL="https://github.com/iina/deps-buildscripts/releases/download/iina-1.5.0-release/iina-deps-fat.zip"
IINA_SHA256="d6b3bb920f14780ec9c9df22e323e2bf72a5ca2b198473c67bf37581f0a60b6e"
MPV_TAG="v0.41.0"
# 头文件名 → sha256（mpv-player/mpv 的 v0.41.0 tag）
MPV_HEADERS=(
  "client.h 3c073236a09cb456c6e80587d5523c8771c32fb22ca014e0f99cc3d43905b75c"
  "render.h 192691941602052f00df0587f126246c48785a1cf21de68d22a92ea1908d1c55"
  "render_gl.h 48662c0ed9872a14dd9e1684105c97f69f94a0414709c254c5d372adc41d2e69"
  "stream_cb.h 188e58b6d14383e15a5dffdc4ecebbdfbf2e412b9c099ff21b571f747a8ce32d"
)

sha256_of() { shasum -a 256 "$1" | cut -d' ' -f1; }

# fetch <url> <目标文件> <期望 sha256>：已存在且哈希对得上就跳过；下载后对不上立即失败、不留文件。
fetch() {
  local url=$1 file=$2 want=$3 got
  if [ -f "$file" ] && [ "$(sha256_of "$file")" = "$want" ]; then
    echo "==> 缓存命中：$(basename "$file")"
    return
  fi
  echo "==> 下载：$url"
  curl -fsSL --retry 3 --retry-delay 2 -o "$file.part" "$url"
  got=$(sha256_of "$file.part")
  if [ "$got" != "$want" ]; then
    rm -f "$file.part"
    echo "sha256 校验失败：$(basename "$file")（期望 $want，实际 $got）" >&2
    exit 1
  fi
  mv "$file.part" "$file"
}

# 下载的东西一律当不可信数据：每个归档放进自己的空目录，解压前先查条目名。
dl="$dest/download"
mkdir -p "$dl/iina-deps-fat" "$dl/mpv-headers"
fetch "$IINA_URL" "$dl/iina-deps-fat/iina-deps-fat.zip" "$IINA_SHA256"
for entry in "${MPV_HEADERS[@]}"; do
  name=${entry%% *}
  fetch "https://raw.githubusercontent.com/mpv-player/mpv/$MPV_TAG/include/mpv/$name" "$dl/mpv-headers/$name" "${entry##* }"
done

# 归档里只能是顶层的 lib*.dylib：带路径、带 .. 或别的名字一律拒绝（防 zip-slip 与夹带）
echo "==> 检查归档条目"
bad=$(unzip -Z1 "$dl/iina-deps-fat/iina-deps-fat.zip" | grep -Ev '^lib[A-Za-z0-9_.+-]+\.dylib$' || true)
if [ -n "$bad" ]; then
  echo "归档里有意料之外的条目，拒绝解压：" >&2
  echo "$bad" >&2
  exit 1
fi

stage=$(mktemp -d "$dest/stage.XXXXXX")
trap 'rm -rf "$stage"' EXIT
unzip -q "$dl/iina-deps-fat/iina-deps-fat.zip" -d "$stage"
if [ -n "$(find "$stage" -mindepth 1 \( -type l -o -type d \))" ]; then
  echo "解压出了符号链接或目录，拒绝使用" >&2
  exit 1
fi
[ -f "$stage/libmpv.2.dylib" ] || { echo "归档里没有 libmpv.2.dylib" >&2; exit 1; }

rm -rf "$dest/lib" "$dest/include"
mkdir -p "$dest/lib" "$dest/include/mpv"
mv "$stage"/*.dylib "$dest/lib/"
for entry in "${MPV_HEADERS[@]}"; do
  cp "$dl/mpv-headers/${entry%% *}" "$dest/include/mpv/"
done

cat > "$dest/DO-NOT-SHIP.txt" <<'EOF'
仅供本机开发：IINA 的预编译 libmpv（iina/deps-buildscripts 没有许可证文件，GPL 合规做不到）。
绝不能复制进 .app、dist/ 或任何 Release。发布版用我们自己构建的 libmpv。
LOCAL DEVELOPMENT ONLY. NEVER SHIP.
EOF

echo "==> 好了：$(ls "$dest/lib" | wc -l | tr -d ' ') 个 dylib 在 $dest/lib"
echo "    构建：scripts/macos/build-desktop-dev.sh（或自己 export 下面两行再 go build -tags libmpv）"
print_env
