#!/usr/bin/env bash
# 生成 Windows 桌面窗口测试用的片子（media/clip.mkv，约 1MB，产物已入库，平时不用跑）：
# 40 秒 640x360 H.264 + AAC（testsrc2 的动态画面，看得出在不在播）+ 一条中日文 ASS 字幕（验 libass 的字体回退）。
# 测试脚本把它复制成「- 01 / - 02 / - 03」三集，媒体库认成同一部作品，才能测「下一集」。
# 需要带 libx264 的 ffmpeg。用法：make-media.sh [ffmpeg 路径]
set -euo pipefail

ff=${1:-ffmpeg}
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cat > "$work/sub.ass" <<'EOF'
[Script Info]
ScriptType: v4.00+
PlayResX: 640
PlayResY: 360

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Microsoft YaHei,26,&H00FFFFFF,&H000000FF,&H00000000,&H64000000,0,0,0,0,100,100,0,0,1,2,0,2,10,10,14,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:00.00,0:00:40.00,Default,,0,0,0,,nagare Windows 测试片 · 字幕テスト
EOF

"$ff" -hide_banner -loglevel error -y -f lavfi -i "testsrc2=s=640x360:r=24:d=40" -f lavfi -i "sine=f=330:r=48000:d=40" \
  -c:v libx264 -preset veryslow -crf 36 -pix_fmt yuv420p -g 96 -c:a aac -b:a 32k -shortest "$work/av.mkv"
mkdir -p "$here/media"
"$ff" -hide_banner -loglevel error -y -i "$work/av.mkv" -i "$work/sub.ass" -map 0 -map 1 -c copy \
  -metadata:s:s:0 language=chi "$here/media/clip.mkv"
ls -l "$here/media/clip.mkv"
