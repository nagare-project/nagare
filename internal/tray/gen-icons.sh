#!/usr/bin/env bash
# 生成托盘图标（可重复执行，仅需在 macOS 上跑一次并把产物提交进仓库）。
#
# 来源：
#   - icon_mac.png   32×32，macOS 菜单栏模板图（纯黑 + alpha，由无底色的 tomoru 断臂光缝图形渲染）
#   - icon_win.ico   Windows 托盘图标（含 16/32/48 三档，直接从 packaging/icon/nagare-256.png 缩放，保留白底以免在深色任务栏隐形）
#   - icon_linux.png Linux 托盘图标（64×64 PNG，直接从 packaging/icon/nagare-256.png 缩放）
#
# 依赖：macOS 自带 qlmanage；python3 + Pillow（PIL）。
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"
app_png="$root/packaging/icon/nagare-256.png"
[[ -f "$app_png" ]] || { echo "缺少 $app_png，请先运行 scripts/icon/build-icons.sh" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# macOS 模板图专用：透明底纯黑轮廓（tomoru DIN 窄高断臂光缝）。
cat > "$work/nagare.svg" <<'SVG'
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
  <path d="M296 764 L468 236 L556 236 L384 764 Z" fill="#000000"/>
  <path d="M574 291 L728 764 L640 764 L530 426 Z" fill="#000000"/>
  <path d="M466 628 L582 696 L466 764 Z" fill="#000000"/>
</svg>
SVG

# 渲染成 256px 的位图；qlmanage 的产物名固定为 <输入名>.png。
qlmanage -t -s 256 -o "$work" "$work/nagare.svg" >/dev/null 2>&1
src="$work/nagare.svg.png"
[[ -f "$src" ]] || { echo "qlmanage 未生成 $src" >&2; exit 1; }

python3 - "$src" "$app_png" "$here" <<'PY'
import sys
from PIL import Image

src, app_png, out = sys.argv[1], sys.argv[2], sys.argv[3]
img = Image.open(src).convert("RGBA")

# qlmanage 缩略图会垫白底：仅用于 macOS 纯黑模板图，把接近纯白的像素判为透明。
px = img.load()
w, h = img.size
for y in range(h):
    for x in range(w):
        r, g, b, a = px[x, y]
        if r > 240 and g > 240 and b > 240:
            px[x, y] = (r, g, b, 0)

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

# Windows / Linux 彩色托盘图：直接由带浅色圆角底的应用图标缩放，明暗任务栏下都清晰可见。
app = Image.open(app_png).convert("RGBA")
app.resize((48, 48), Image.LANCZOS).save(f"{out}/icon_win.ico", sizes=[(16, 16), (32, 32), (48, 48)])
app.resize((64, 64), Image.LANCZOS).save(f"{out}/icon_linux.png", optimize=True)
print("已生成 icon_mac.png、icon_win.ico 与 icon_linux.png")
PY
