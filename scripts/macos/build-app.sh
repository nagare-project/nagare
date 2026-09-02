#!/usr/bin/env bash
# 把 goreleaser 产出的 universal 二进制组装成 Nagare.app（ad-hoc 签名）并打成 dmg。
# 由 .goreleaser.yaml 的 universal_binaries post hook 调用，也可手动运行。
# 用法：scripts/macos/build-app.sh <universal 二进制路径> <版本> <输出目录>
# 产物：<输出目录>/Nagare.app 与 <输出目录>/nagare-<版本>_MacOS_universal.dmg
#
# 零证书方案：ad-hoc 签名（identity "-"）没有 Apple 开发者证书，用户首次打开会被
# Gatekeeper 拦下，需要在「系统设置 › 隐私与安全性」里点「仍要打开」；README 有说明。
set -euo pipefail

if [ $# -ne 3 ]; then
  echo "用法：$0 <二进制路径> <版本> <输出目录>" >&2
  exit 2
fi
bin=$1
version=$2
mkdir -p "$3"
out=$(cd "$3" && pwd)
root=$(cd "$(dirname "$0")/../.." && pwd)

for tool in codesign hdiutil plutil lipo; do
  command -v "$tool" >/dev/null || { echo "缺少 $tool（本脚本只能在 macOS 上运行）" >&2; exit 1; }
done
[ -f "$bin" ] || { echo "二进制不存在：$bin" >&2; exit 1; }
[ -f "$root/packaging/icon/nagare.icns" ] || { echo "缺少 packaging/icon/nagare.icns，先跑 scripts/icon/build-icons.sh" >&2; exit 1; }

echo "==> 二进制架构：$(lipo -archs "$bin")"

app="$out/Nagare.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
sed "s/__VERSION__/$version/g" "$root/scripts/macos/Info.plist.tmpl" > "$app/Contents/Info.plist"
plutil -lint "$app/Contents/Info.plist" >/dev/null
printf 'APPL????' > "$app/Contents/PkgInfo"
cp "$bin" "$app/Contents/MacOS/nagare"
chmod 755 "$app/Contents/MacOS/nagare"
cp "$root/packaging/icon/nagare.icns" "$app/Contents/Resources/nagare.icns"

echo "==> ad-hoc 签名"
codesign --force --sign - "$app"
codesign --verify --deep --strict --verbose=2 "$app"

dmg="$out/nagare-${version}_MacOS_universal.dmg"
rm -f "$dmg"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
cp -R "$app" "$stage/"
ln -s /Applications "$stage/Applications"

echo "==> 打包 dmg：$dmg"
# GitHub 的 macOS runner 上 hdiutil 偶发 "Resource busy"，重试三次。
for attempt in 1 2 3; do
  if hdiutil create -volname nagare -srcfolder "$stage" -ov -format UDZO -quiet "$dmg"; then
    break
  fi
  [ "$attempt" -lt 3 ] || { echo "hdiutil create 连续失败" >&2; exit 1; }
  echo "hdiutil create 失败，${attempt}s 后重试…" >&2
  sleep "$attempt"
done

echo "==> 验证 dmg 可挂载且签名为 adhoc"
mnt=$(mktemp -d)
hdiutil attach "$dmg" -nobrowse -readonly -quiet -mountpoint "$mnt"
sig=$(codesign -dv "$mnt/Nagare.app" 2>&1 || true)
hdiutil detach "$mnt" -quiet
rmdir "$mnt" 2>/dev/null || true
if ! grep -q 'Signature=adhoc' <<<"$sig"; then
  echo "dmg 内 Nagare.app 的签名不是 adhoc：" >&2
  echo "$sig" >&2
  exit 1
fi
echo "==> 完成：$app"
echo "==> 完成：$dmg"
