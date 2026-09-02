#!/usr/bin/env bash
# 从 packaging/icon/nagare.svg 生成三平台图标产物（全部入库，体积很小）：
#   nagare.icns      macOS .app 图标（iconutil）
#   nagare.ico       Windows 安装包 / exe 图标（16/32/48/256）
#   nagare-256.png   Linux hicolor 图标
# 只能在 macOS 上跑（依赖 qlmanage、iconutil、python3 + Pillow），CI 不需要执行；
# 改了 SVG 之后重新运行一次并把产物一起提交即可。可重复执行。
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
icon_dir="$root/packaging/icon"
svg="$icon_dir/nagare.svg"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for tool in qlmanage iconutil python3; do
  command -v "$tool" >/dev/null || { echo "缺少 $tool（本脚本只能在 macOS 上运行）" >&2; exit 1; }
done
python3 -c 'import PIL' 2>/dev/null || { echo "python3 缺少 Pillow：pip install pillow" >&2; exit 1; }

# qlmanage 用 WebKit 渲染 SVG，字体走系统 CJK 字体（PingFang SC），输出 <name>.svg.png。
qlmanage -t -s 1024 -o "$work" "$svg" >/dev/null 2>&1
master="$work/$(basename "$svg").png"
[ -f "$master" ] || { echo "qlmanage 渲染失败：$svg" >&2; exit 1; }

python3 - "$master" "$work/nagare.iconset" "$icon_dir" <<'PY'
import os
import sys
from PIL import Image

master, iconset, out = sys.argv[1:]
im = Image.open(master).convert("RGBA")
if im.size != (1024, 1024):
    im = im.resize((1024, 1024), Image.LANCZOS)

def scaled(n):
    return im.resize((n, n), Image.LANCZOS)

# iconutil 要求的命名：icon_<n>x<n>.png 与 icon_<n>x<n>@2x.png
os.makedirs(iconset, exist_ok=True)
for base in (16, 32, 128, 256, 512):
    scaled(base).save(os.path.join(iconset, f"icon_{base}x{base}.png"))
    scaled(base * 2).save(os.path.join(iconset, f"icon_{base}x{base}@2x.png"))

# Windows .ico：256 那层由 Pillow 以 PNG 压缩写入，Vista+ 都认
scaled(256).save(os.path.join(out, "nagare.ico"), format="ICO",
                 sizes=[(16, 16), (32, 32), (48, 48), (256, 256)])
scaled(256).save(os.path.join(out, "nagare-256.png"))
PY

iconutil -c icns "$work/nagare.iconset" -o "$icon_dir/nagare.icns"
ls -la "$icon_dir"
