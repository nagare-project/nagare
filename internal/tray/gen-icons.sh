#!/usr/bin/env bash
# 生成托盘图标（可重复执行，仅需在 macOS 上跑一次并把产物提交进仓库）。
#
# 来源：frontend/index.html 里的内联 SVG favicon（「流」字，LIVE Cyan #5ac8fa）。
# 流程：SVG → qlmanage 渲染成 PNG → python3 + PIL 派生两份产物：
#   icon_mac.png  32×32，macOS 菜单栏模板图（纯黑 + alpha，系统按明暗主题自动着色）
#   icon_win.ico  Windows 托盘图标（含 16/32/48 三档，保留原色）
#
# 依赖：macOS 自带 qlmanage；python3 + Pillow（PIL）。
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# 与 frontend/index.html 的 favicon 完全一致（仅做了 URL 解码）。
cat > "$work/nagare.svg" <<'SVG'
<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='0.86em' font-size='84' fill='#5ac8fa'>流</text></svg>
SVG

# 渲染成 256px 的位图；qlmanage 的产物名固定为 <输入名>.png。
qlmanage -t -s 256 -o "$work" "$work/nagare.svg" >/dev/null 2>&1
src="$work/nagare.svg.png"
[[ -f "$src" ]] || { echo "qlmanage 未生成 $src" >&2; exit 1; }

python3 - "$src" "$here" <<'PY'
import sys
from PIL import Image

src, out = sys.argv[1], sys.argv[2]
img = Image.open(src).convert("RGBA")

# qlmanage 有时会铺白底：把接近纯白的像素判为透明，只保留字形。
px = img.load()
w, h = img.size
for y in range(h):
    for x in range(w):
        r, g, b, a = px[x, y]
        if r > 240 and g > 240 and b > 240:
            px[x, y] = (r, g, b, 0)

# 裁到字形的包围盒再等比缩放，避免图标在托盘里显得偏小。
bbox = img.getbbox()
if bbox is None:
    sys.exit("渲染结果为空：没有任何不透明像素")
glyph = img.crop(bbox)

def fit(image, size):
    """把 image 等比缩进 size×size 的透明画布并居中。"""
    scale = size / max(image.size)
    resized = image.resize(
        (max(1, round(image.width * scale)), max(1, round(image.height * scale))),
        Image.LANCZOS,
    )
    canvas = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    canvas.paste(resized, ((size - resized.width) // 2, (size - resized.height) // 2), resized)
    return canvas

# macOS 模板图：颜色一律置黑，只保留 alpha —— 系统负责按菜单栏明暗着色。
mac = fit(glyph, 32)
alpha = mac.getchannel("A")
template = Image.new("RGBA", mac.size, (0, 0, 0, 255))
template.putalpha(alpha)
template.save(f"{out}/icon_mac.png", optimize=True)

# Windows ICO：保留原色，内含 16/32/48 三档。
win = fit(glyph, 48)
win.save(f"{out}/icon_win.ico", sizes=[(16, 16), (32, 32), (48, 48)])
print("已生成 icon_mac.png 与 icon_win.ico")
PY
