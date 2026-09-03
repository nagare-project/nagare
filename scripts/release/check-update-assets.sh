#!/usr/bin/env bash
# 校验发布产物里有客户端自更新会去下载的那几个名字。
#
# 为什么需要这一步：归档命名在两处各写了一遍 —— .goreleaser.yaml 的 name_template
# （加上 scripts/macos/build-app.sh 的 .app.zip）与 internal/selfupdate/assets.go 的
# assetTemplate —— 没有任何东西把它们绑在一起。哪天改了模板，界面照样显示「有新
# 版本」、按钮照样能点，然后只在那个平台上报「这个版本没有适用于当前平台的更新包」。
# 让它在发布时就红，而不是在用户机器上。
#
# 用法：scripts/release/check-update-assets.sh [校验清单路径]
set -euo pipefail

manifest=${1:-dist/checksums.txt}
[ -f "$manifest" ] || { echo "找不到校验清单：$manifest" >&2; exit 1; }

# 与 internal/selfupdate/assets.go 的 assetTemplate 一一对应。
# 只比后缀，与版本号无关。
suffixes=(
  _MacOS_universal.app.zip # ChannelAppBundle（dmg 装的）
  _MacOS_universal.tar.gz  # macOS 直接跑裸二进制
  _Linux_x86_64.tar.gz
  _Linux_arm64.tar.gz
  _Windows_x86_64.zip
)

missing=0
for suffix in "${suffixes[@]}"; do
  if grep -qE -- "${suffix//./\\.}\$" "$manifest"; then
    echo "  ✓ *${suffix}"
  else
    echo "  ✗ 校验清单里没有以 ${suffix} 结尾的产物" >&2
    missing=1
  fi
done

if [ "$missing" -ne 0 ]; then
  echo "自更新会在对应平台上失效。对照 internal/selfupdate/assets.go 的 assetTemplate 修。" >&2
  exit 1
fi
echo "==> 自更新要用的产物齐了"
