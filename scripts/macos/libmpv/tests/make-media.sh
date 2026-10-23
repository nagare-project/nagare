#!/usr/bin/env bash
# 生成 verify.sh 用的测试片段（tests/media/）。产物已入库，平时不用跑；
# 只有改了测试矩阵时才需要重新生成，然后把新文件一起提交。
#
# 需要一个带全套编码器的 ffmpeg（≥ 6：libx264 / libx265 / libsvtav1 / libvpx / libopus /
# libmp3lame），例如 Homebrew 的 ffmpeg。我们自己构建的 FFmpeg 只留了截图用的编码器，生成不了这些。
# 用法：tests/make-media.sh [ffmpeg 路径] [ext]（ext：只重新生成按扩展名补的那几段，见 ext_media）
set -euo pipefail

ff=${1:-ffmpeg}
here=$(cd "$(dirname "$0")" && pwd)
out="$here/media"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$out"

common=(-hide_banner -loglevel error -y -fflags +bitexact)
# 5 秒 24fps = 120 帧，够 verify.sh 的 --frames=100；分辨率压到最小，几个文件加起来几百 KB
video=(-f lavfi -i "testsrc2=s=160x96:r=24:d=5")
audio=(-f lavfi -i "sine=f=440:r=48000:d=5")
# 无损编码（FLAC / TrueHD / PCM）用静音：解码路径一样走到，文件小两个数量级
silence=(-f lavfi -i "anullsrc=r=48000:cl=stereo:d=5")

# 字幕：ASS 里放中日文，验证 libass + CoreText 的字体回退（系统里没有 Arial Unicode 也要能找到 CJK 字体）
cat > "$work/sub.ass" <<'EOF'
[Script Info]
ScriptType: v4.00+
PlayResX: 160
PlayResY: 96

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Hiragino Sans,18,&H00FFFFFF,&H000000FF,&H00000000,&H64000000,0,0,0,0,100,100,0,0,1,2,0,2,8,8,8,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:00.00,0:00:05.00,Default,,0,0,0,,流れ nagare 字幕テスト
EOF
cat > "$work/sub.srt" <<'EOF'
1
00:00:00,000 --> 00:00:05,000
srt 字幕
EOF
cat > "$work/sub.vtt" <<'EOF'
WEBVTT

00:00:00.000 --> 00:00:05.000
webvtt 字幕
EOF

x264=(-c:v libx264 -preset veryfast -crf 40 -pix_fmt yuv420p -g 24)

# 媒体库扫描器认的其余扩展名（internal/library/scan.go 的 scanVideoExts），每个一段：老格式挑 ffmpeg 编得出的
# 那一种（RV30/40、cook、WMV 9、VP6 编不出来，verify.sh 只能查组件在不在）。
# 只重新生成这几个：tests/make-media.sh <ffmpeg> ext
ext_media() {
  "$ff" "${common[@]}" "${video[@]}" "${audio[@]}" \
    "${x264[@]}" -c:a alac -ac 2 "$out/h264-alac.mov"
  "$ff" "${common[@]}" "${video[@]}" "${audio[@]}" \
    -f mp4 -c:v mpeg4 -q:v 31 -c:a aac -b:a 48k -ac 2 "$out/mpeg4-aac.m4v"
  "$ff" "${common[@]}" "${video[@]}" -f lavfi -i "sine=f=440:r=22050:d=5" \
    -c:v flv -q:v 31 -c:a adpcm_swf -ac 1 "$out/flv1-adpcm.flv"
  "$ff" "${common[@]}" "${video[@]}" "${audio[@]}" \
    -c:v wmv2 -q:v 31 -c:a wmav2 -b:a 64k -ac 2 "$out/wmv2-wmav2.wmv"
  "$ff" "${common[@]}" "${video[@]}" -f lavfi -i "sine=f=440:r=8000:d=5" \
    -f rm -c:v rv20 -q:v 31 -c:a real_144 -ac 1 -ar 8000 "$out/rv20-ra144.rmvb"
}
if [ "${2:-}" = ext ]; then
  ext_media
  ls -l "$out"
  exit 0
fi

"$ff" "${common[@]}" "${video[@]}" "${audio[@]}" \
  -i "$work/sub.ass" -i "$work/sub.srt" -i "$work/sub.vtt" \
  -map 0:v -map 1:a -map 2 -map 3 -map 4 \
  "${x264[@]}" -c:a aac -b:a 48k -ac 2 -c:s:0 ass -c:s:1 subrip -c:s:2 webvtt \
  "$out/h264-aac-ass.mkv"

"$ff" "${common[@]}" "${video[@]}" "${silence[@]}" -shortest \
  -c:v libx265 -preset fast -crf 40 -pix_fmt yuv420p10le -profile:v main10 -x265-params log-level=error \
  -c:a flac "$out/hevc10-flac.mkv"

"$ff" "${common[@]}" "${video[@]}" "${audio[@]}" \
  -c:v libsvtav1 -preset 10 -crf 50 -pix_fmt yuv420p10le -svtav1-params loglevel=0 \
  -c:a libopus -b:a 32k "$out/av1-opus.webm"

"$ff" "${common[@]}" "${video[@]}" "${audio[@]}" \
  -c:v libvpx-vp9 -deadline realtime -cpu-used 8 -crf 50 -b:v 0 \
  -c:a vorbis -strict -2 -ac 2 "$out/vp9-vorbis.webm"

"$ff" "${common[@]}" "${video[@]}" "${audio[@]}" \
  -c:v mpeg2video -q:v 31 -c:a ac3 -b:a 64k "$out/mpeg2-ac3.ts"

"$ff" "${common[@]}" "${video[@]}" "${audio[@]}" -i "$work/sub.srt" \
  -map 0:v -map 1:a -map 2 \
  "${x264[@]}" -c:a eac3 -b:a 64k -c:s mov_text "$out/h264-eac3-movtext.mp4"

"$ff" "${common[@]}" "${video[@]}" "${audio[@]}" \
  -c:v mpeg4 -vtag xvid -q:v 31 -c:a libmp3lame -b:a 32k "$out/mpeg4-mp3.avi"

ext_media

# 音频轨：DTS / TrueHD / PCM 24bit 各一条（mpv 用 --aid 逐条解）
a2=(-f lavfi -i "sine=f=330:r=48000:d=1")
a2s=(-f lavfi -i "anullsrc=r=48000:cl=stereo:d=1")
"$ff" "${common[@]}" "${a2[@]}" "${a2s[@]}" "${a2s[@]}" \
  -map 0 -map 1 -map 2 \
  -c:a:0 dca -strict -2 -b:a:0 320k -ac:a:0 2 -c:a:1 truehd -strict -2 -c:a:2 pcm_s24le -ar:a:2 8000 -ac:a:2 1 \
  "$out/audio-dts-truehd-pcm.mka"

# HLS：本机 https 服务端提供，验证 TLS + hls 分离器 + mpegts
mkdir -p "$out/hls"
rm -f "$out/hls/"*
"$ff" "${common[@]}" "${video[@]}" "${audio[@]}" \
  "${x264[@]}" -c:a aac -b:a 48k -ac 2 \
  -f hls -hls_time 1 -hls_list_size 0 -hls_playlist_type vod \
  -hls_segment_filename "$out/hls/seg%d.ts" "$out/hls/index.m3u8"

ls -l "$out" "$out/hls"
