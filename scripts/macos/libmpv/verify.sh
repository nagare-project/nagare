#!/usr/bin/env bash
# 验证 package.sh universal 产出的那一套文件。任何一项「必过」失败都以非零退出。
#
# 用法：verify.sh <产物目录> [--static-only]
#
# 静态（tools/macho.py check）：符号链接、universal、minos ≤ 13.0、装载名、LC_RPATH、依赖、签名、头文件；
#   另核对 FFmpeg 的组件清单与 mpv 的特性列表（两个架构都看）。
# 运行（在本机架构上跑全套；装了 Rosetta 时 x86_64 切片再跑一遍主要几项）：
#   mpv --version / 解码各测试片段 100 帧 / 各音轨 / 字幕确实画到画面上 / jpg 与 png 截图 /
#   thumbfast 那套编码参数 / Lua / nagare 自己的界面脚本 / 本机 HTTPS（含证书校验与 HLS）/
#   cocoa-cb 窗口 / 一个链接 libmpv 的 C 程序（经 @rpath 装载 + render API 画帧）。
# 标成「可选」的项目失败只发警告：它们依赖 runner 有没有 GPU / VideoToolbox / 图形会话。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib/env.sh
. "$here/lib/env.sh"
# shellcheck source=lib/sources.sh
. "$here/lib/sources.sh"

[ $# -ge 1 ] || die "用法：verify.sh <产物目录> [--static-only]"
OUT=$(cd "$1" && pwd)
static_only=false
[ "${2:-}" = --static-only ] && static_only=true

MEDIA="$here/tests/media"
# 各项的日志放这里。设了 LIBMPV_VERIFY_DIR 就用它且不删（CI 失败时上传），否则用临时目录。
# 用真实路径（/private/var/...）：dyld 打印的是解析后的路径，比对包内路径时两边要一致
if [ -n "${LIBMPV_VERIFY_DIR:-}" ]; then
  rm -rf "$LIBMPV_VERIFY_DIR"
  mkdir -p "$LIBMPV_VERIFY_DIR"
  TMP=$(cd "$LIBMPV_VERIFY_DIR" && pwd -P)
  keep_tmp=true
else
  TMP=$(cd "$(mktemp -d)" && pwd -P)
  keep_tmp=false
fi
SERVER_PID=""
cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  $keep_tmp || rm -rf "$TMP"
}
trap cleanup EXIT

passed=0
failed=0
warned=0
failures=""
pass() { passed=$((passed + 1)); printf '  ✓ %s\n' "$1"; }
fail() { failed=$((failed + 1)); failures="$failures\n  - $1"; printf '  ✗ %s\n' "$1"; }
soft() { warned=$((warned + 1)); warn "可选项未通过：$1"; }

# with_timeout <秒> 命令...（macOS 没有 timeout，用 perl 的 alarm）
with_timeout() {
  local secs=$1
  shift
  perl -e 'alarm shift; exec @ARGV or die "exec: $!"' "$secs" "$@"
}

# mpv_run <架构> <日志> 参数...：跑产物里的 mpv，输出进日志，返回 mpv 的退出码
mpv_run() {
  local arch=$1 logf=$2
  shift 2
  local pre=()
  [ "$arch" = "$HOST_ARCH" ] || pre=(arch "-$arch")
  set +e
  with_timeout 120 ${pre[@]+"${pre[@]}"} "$OUT/MacOS/mpv" "$@" > "$logf" 2>&1
  local rc=$?
  set -e
  return $rc
}

show_log() { sed 's/^/      | /' "$1" | tail -n "${2:-25}"; }

# ---------------------------------------------------------------------------
echo "== 静态检查 =="
python3 -I -B "$here/tools/macho.py" check "$OUT" --minos "$DEPLOYMENT_TARGET" || fail "Mach-O 静态检查"

# 需求里点名的 FFmpeg 组件（spec，与 recipes.sh 里的开关清单互相独立：这里列的是「必须能用」的）
REQUIRED_FF="
h264_decoder hevc_decoder libdav1d_decoder av1_decoder vp9_decoder vp8_decoder
aac_decoder flac_decoder opus_decoder vorbis_decoder ac3_decoder eac3_decoder truehd_decoder dca_decoder
mp3_decoder mp3float_decoder pcm_s16le_decoder pcm_s24le_decoder pcm_bluray_decoder
ass_decoder subrip_decoder webvtt_decoder pgssub_decoder movtext_decoder
matroska_demuxer mov_demuxer mpegts_demuxer hls_demuxer ass_demuxer srt_demuxer webvtt_demuxer sup_demuxer
h264_videotoolbox_hwaccel hevc_videotoolbox_hwaccel vp9_videotoolbox_hwaccel av1_videotoolbox_hwaccel
mjpeg_encoder png_encoder rawvideo_encoder image2_muxer
file_protocol http_protocol https_protocol tls_protocol tcp_protocol crypto_protocol
scale_filter crop_filter pad_filter format_filter hflip_filter vflip_filter zscale_filter tonemap_filter
"
# 需求里点名要丢掉的
FORBIDDEN_FF="libx264_encoder libx265_encoder h264_videotoolbox_encoder aac_encoder bluray_protocol"
for arch in arm64 x86_64; do
  comps="$OUT/info/$arch/ffmpeg-components.txt"
  if [ ! -s "$comps" ]; then fail "缺少 info/$arch/ffmpeg-components.txt"; continue; fi
  missing=""
  for c in $REQUIRED_FF; do grep -qx "$c" "$comps" || missing="$missing $c"; done
  [ -z "$missing" ] && pass "FFmpeg 组件齐全（${arch}，$(wc -l < "$comps" | tr -d ' ') 个）" || fail "FFmpeg（${arch}）缺少：$missing"
  extra=""
  for c in $FORBIDDEN_FF; do grep -qx "$c" "$comps" && extra="$extra $c"; done
  [ -z "$extra" ] && pass "FFmpeg 没有多余的编码器（${arch}）" || fail "FFmpeg（${arch}）不该有：$extra"
  if grep -q '^License: LGPL version 3 or later' "$OUT/info/$arch/ffmpeg-configure-summary.txt"; then
    pass "FFmpeg 许可证 LGPL v3+（${arch}）"
  else
    fail "FFmpeg 许可证不是预期的 LGPL v3+（${arch}）：$(grep '^License' "$OUT/info/$arch/ffmpeg-configure-summary.txt")"
  fi
done

REQUIRED_MPV_FEATURES="libmpv-shared cplayer luajit libass libplacebo gl gl-cocoa videotoolbox-gl macos-cocoa-cb cocoa swift
coreaudio lcms2 uchardet jpeg zlib zimg iconv gpl"
for arch in arm64 x86_64; do
  feats=" $(tr -d '"' < "$OUT/info/$arch/mpv-features.txt") "
  missing=""
  for f in $REQUIRED_MPV_FEATURES; do
    case "$f" in libmpv-shared | cplayer) continue ;; esac # 这两个不在 config.h 的列表里，下面运行时看
    case "$feats" in *" $f "*) ;; *) missing="$missing $f" ;; esac
  done
  [ -z "$missing" ] && pass "mpv 特性齐全（${arch}）" || fail "mpv（${arch}）缺少特性：$missing"
  for f in javascript vulkan libbluray libarchive rubberband sdl2-video; do
    case "$feats" in *" $f "*) fail "mpv（${arch}）不该开 $f" ;; esac
  done
done

if $static_only; then
  echo "静态检查：通过 ${passed}，失败 $failed"
  [ $failed -eq 0 ] || { printf "失败项：$failures\n" >&2; exit 1; }
  exit 0
fi

# ---------------------------------------------------------------------------
want_mpv=$(lock_field mpv 2)
want_ffmpeg=$(lock_field ffmpeg 2)
want_placebo=$(lock_field libplacebo 2)

arches="$HOST_ARCH"
if [ "$HOST_ARCH" = arm64 ]; then
  if can_run_arch x86_64; then arches="arm64 x86_64"; else warn "没有 Rosetta，x86_64 切片只做了静态检查"; fi
fi

# 播一个文件：退出码 0，VO / AO 起来了，没有解码器报错
play_ok() {
  local arch=$1 name=$2 logf
  shift 2
  logf="$TMP/play-$arch-$name.log"
  if ! mpv_run "$arch" "$logf" --no-config --vo=null --ao=null --hwdec=no --msg-level=all=v "$@"; then
    fail "播放 ${name}（${arch}）：mpv 退出码非零"; show_log "$logf"; return
  fi
  if grep -q -e 'Failed to initialize a decoder' -e 'Could not open codec' -e 'No video or audio streams' "$logf"; then
    fail "播放 ${name}（${arch}）：解码器报错"; grep -e 'Failed' -e 'Could not' "$logf" | head -5; return
  fi
  pass "播放 ${name}（${arch}）"
}

for arch in $arches; do
  echo "== 运行检查（${arch}）=="
  logf="$TMP/version-$arch.log"
  if mpv_run "$arch" "$logf" --no-config --version && grep -q "^mpv v$want_mpv" "$logf" \
    && grep -q "FFmpeg version: $want_ffmpeg" "$logf" && grep -q "libplacebo version: v$want_placebo" "$logf"; then
    pass "mpv --version：$(head -1 "$logf")"
  else
    fail "mpv --version（${arch}）与 versions.lock 不符"; show_log "$logf"
  fi

  for f in h264-aac-ass.mkv hevc10-flac.mkv av1-opus.webm vp9-vorbis.webm; do
    play_ok "$arch" "$f" --frames=100 "$MEDIA/$f"
    grep -q 'VO: \[null\]' "$TMP/play-$arch-$f.log" || fail "${f}（${arch}）：视频没有输出"
    grep -q 'AO: \[null\]' "$TMP/play-$arch-$f.log" || fail "${f}（${arch}）：音频没有输出"
  done

  logf="$TMP/lua-$arch.log"
  jit_arch=arm64
  [ "$arch" = x86_64 ] && jit_arch=x64
  if mpv_run "$arch" "$logf" --no-config --idle=no --vo=null --ao=null --script="$here/tests/lua_check.lua" \
    "$MEDIA/h264-aac-ass.mkv" && grep -q "lua-check engine=LuaJIT_2\.1[^ ]* arch=$jit_arch " "$logf"; then
    pass "Lua：$(grep -o 'lua-check engine=[^ ]* arch=[^ ]*' "$logf" | head -1)"
  else
    fail "Lua 检查（${arch}）"; show_log "$logf"
  fi
done

arch=$HOST_ARCH
echo "== 完整解码与功能检查（${arch}）=="
for f in mpeg2-ac3.ts h264-eac3-movtext.mp4 mpeg4-mp3.avi; do
  play_ok "$arch" "$f" --frames=100 "$MEDIA/$f"
done
for aid in 1 2 3; do
  play_ok "$arch" "mka-aid$aid" --aid="$aid" --end=0.5 "$MEDIA/audio-dts-truehd-pcm.mka"
done
grep -q 'AO: \[null\]' "$TMP/play-$arch-mka-aid2.log" || fail "TrueHD 音轨没有输出"

logf="$TMP/features.log"
mpv_run "$arch" "$logf" --no-config -v --vo=null --ao=null --frames=1 "$MEDIA/h264-aac-ass.mkv" || true
if grep -q 'List of enabled features:' "$logf"; then
  pass "运行时特性：$(grep 'List of enabled features:' "$logf" | head -1 | cut -c1-160)…"
else
  fail "读不到运行时特性列表"
fi

logf="$TMP/protocols.log"
mpv_run "$arch" "$logf" --no-config --list-protocols || true
grep -qx '[[:space:]]*https://' "$logf" && pass "协议含 https" || { fail "--list-protocols 里没有 https"; show_log "$logf"; }

logf="$TMP/hwdec.log"
mpv_run "$arch" "$logf" --no-config --hwdec=help || true
grep -q 'videotoolbox-copy' "$logf" && grep -q '^ *videotoolbox' "$logf" \
  && pass "hwdec 可选 videotoolbox / videotoolbox-copy" || { fail "--hwdec=help 里没有 videotoolbox"; show_log "$logf"; }

# 字幕确实画上去了：同一帧，带字幕与不带字幕的截图必须不同（vo=image 会把字幕画进输出图像）
sub_drawn() {
  local id=$1 label=$2 file=$3 sid=$4
  local a="$TMP/sub-$id-on" b="$TMP/sub-$id-off"
  mkdir -p "$a" "$b"
  mpv_run "$arch" "$a.log" --no-config --vo=image --vo-image-format=png --vo-image-outdir="$a" \
    --ao=null --hwdec=no --start=1 --frames=1 --sid="$sid" "$file" || true
  mpv_run "$arch" "$b.log" --no-config --vo=image --vo-image-format=png --vo-image-outdir="$b" \
    --ao=null --hwdec=no --start=1 --frames=1 --sid=no "$file" || true
  if [ -s "$a/00000001.png" ] && [ -s "$b/00000001.png" ] && ! cmp -s "$a/00000001.png" "$b/00000001.png"; then
    pass "字幕渲染：$label"
  else
    fail "字幕渲染：${label}（带字幕与不带字幕的画面相同，或没有输出）"; show_log "$a.log" 10
  fi
}
sub_drawn ass "ASS（libass + CoreText 字体回退）" "$MEDIA/h264-aac-ass.mkv" 1
sub_drawn srt "SubRip" "$MEDIA/h264-aac-ass.mkv" 2
sub_drawn vtt "WebVTT" "$MEDIA/h264-aac-ass.mkv" 3
sub_drawn movtext "mov_text（mp4）" "$MEDIA/h264-eac3-movtext.mp4" 1

# 截图格式：mpv 默认 jpg（libjpeg），也测 png
for fmt in jpg png; do
  d="$TMP/shot-$fmt"
  mkdir -p "$d"
  mpv_run "$arch" "$TMP/shot-$fmt.log" --no-config --vo=image --vo-image-format="$fmt" --vo-image-outdir="$d" \
    --ao=null --frames=1 "$MEDIA/hevc10-flac.mkv" || true
  magic=$(head -c 4 "$d/00000001.$fmt" 2>/dev/null | xxd -p)
  case "$fmt:$magic" in
    jpg:ffd8ff*) pass "截图 jpg" ;;
    png:89504e47) pass "截图 png" ;;
    *) fail "截图 ${fmt}（文件头 ${magic}）"; show_log "$TMP/shot-$fmt.log" 10 ;;
  esac
done

# thumbfast 那套参数：裁剪 / 缩放 / 补边 / 翻转 + rawvideo 编码进 image2，再走一遍 zscale（zimg）
thumb() {
  local label=$1 vf=$2 file=$3 out="$TMP/thumb-$1.raw"
  mpv_run "$arch" "$TMP/thumb-$label.log" --no-config --no-audio --no-sub --hwdec=no --start=1 --frames=1 \
    --vf="$vf" --ovc=rawvideo --of=image2 --ofopts=update=1 --o="$out" "$file" || true
  local size
  size=$(stat -f %z "$out" 2>/dev/null || echo 0)
  if [ "$size" = $((64 * 40 * 4)) ]; then pass "thumbfast 编码：$label"; else
    fail "thumbfast 编码：${label}（输出 $size 字节，应为 $((64 * 40 * 4))）"; show_log "$TMP/thumb-$label.log" 10
  fi
}
thumb swscale "lavfi-crop=w=120:h=72:x=0:y=0,scale=w=64:h=40,pad=w=64:h=40:x=-1:y=-1,format=bgra,hflip,vflip" \
  "$MEDIA/h264-aac-ass.mkv"
thumb zimg "lavfi=[zscale=w=64:h=40],format=bgra" "$MEDIA/hevc10-flac.mkv"

# nagare 自己的界面脚本（internal/mpv/ui）在这个 LuaJIT 上能加载、不报错。
# 加载成功的标志：脚本起来后会把 mpv 自带的控制条设成 never。源码包里没有 internal/，那里跳过
logf="$TMP/nagare-ui.log"
if [ ! -d "$REPO_ROOT/internal/mpv/ui" ]; then
  soft "没有 internal/mpv/ui（源码包里不带），跳过界面脚本检查"
elif mpv_run "$arch" "$logf" --config-dir="$REPO_ROOT/internal/mpv/ui" --vo=null --ao=null --frames=48 \
  --msg-level=all=v "$MEDIA/h264-aac-ass.mkv" && grep -q 'user-data/osc/visibility="never"' "$logf" \
  && ! grep -q -i -e 'lua error' -e 'stack traceback' "$logf"; then
  pass "nagare 界面脚本在 LuaJIT 下加载无错"
else
  fail "nagare 界面脚本"; grep -i -B2 -A8 -e 'lua error' -e 'traceback' "$logf" | head -30 || show_log "$logf"
fi

# 本机 HTTPS：自签 CA → 服务端证书（CN/SAN = localhost），mpv 用 --tls-ca-file 校验
echo "== HTTPS（mbedTLS）=="
cat > "$TMP/ca.cnf" <<'EOF'
[req]
distinguished_name = dn
prompt = no
[dn]
CN = nagare libmpv test CA
[v3_ca]
basicConstraints = critical, CA:TRUE
keyUsage = critical, keyCertSign, cRLSign
subjectKeyIdentifier = hash
EOF
cat > "$TMP/srv.ext" <<'EOF'
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = DNS:localhost
EOF
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -keyout "$TMP/ca.key" -out "$TMP/ca.pem" \
  -config "$TMP/ca.cnf" -extensions v3_ca 2>/dev/null
openssl req -newkey rsa:2048 -nodes -keyout "$TMP/srv.key" -out "$TMP/srv.csr" -subj /CN=localhost 2>/dev/null
openssl x509 -req -in "$TMP/srv.csr" -CA "$TMP/ca.pem" -CAkey "$TMP/ca.key" -CAcreateserial -days 2 \
  -extfile "$TMP/srv.ext" -out "$TMP/srv.pem" 2>/dev/null
python3 -I -B "$here/tests/https_server.py" "$MEDIA" "$TMP/srv.pem" "$TMP/srv.key" "$TMP/port" \
  > "$TMP/https-server.log" 2>&1 &
SERVER_PID=$!
# runner 上第一次启动 python 时系统的安全扫描可能要好几秒，多等一会
for _ in $(seq 1 600); do [ -s "$TMP/port" ] && break; sleep 0.1; done
if [ ! -s "$TMP/port" ]; then
  kill -0 "$SERVER_PID" 2>/dev/null && echo "（服务端进程仍在运行）"
  die "HTTPS 测试服务端 60 秒内没起来：$(cat "$TMP/https-server.log")"
fi
base="https://localhost:$(cat "$TMP/port")"

https_case() {
  local label=$1 expect=$2 url=$3
  shift 3
  local logf="$TMP/https-$label.log" rc=0
  mpv_run "$arch" "$logf" --no-config --vo=null --ao=null --hwdec=no --frames=24 "$@" "$url" || rc=$?
  if [ "$expect" = ok ] && [ $rc -eq 0 ] && grep -q 'VO: \[null\]' "$logf"; then pass "HTTPS：$label"
  elif [ "$expect" = reject ] && [ $rc -ne 0 ]; then pass "HTTPS：${label}（按预期拒绝）"
  else fail "HTTPS：${label}（退出码 ${rc}）"; show_log "$logf" 15; fi
}
https_case "校验证书 + 自定 CA" ok "$base/h264-aac-ass.mkv" --tls-verify=yes --tls-ca-file="$TMP/ca.pem"
https_case "HLS 经 HTTPS" ok "$base/hls/index.m3u8" --tls-verify=yes --tls-ca-file="$TMP/ca.pem"
https_case "不信任的证书" reject "$base/h264-aac-ass.mkv" --tls-verify=yes
https_case "不校验证书（mpv 默认）" ok "$base/h264-aac-ass.mkv" --tls-verify=no

# ---------------------------------------------------------------------------
echo "== 嵌入（C 程序链接 libmpv）=="
bundle="$TMP/Fake.app/Contents"
mkdir -p "$bundle/MacOS"
cp -R "$OUT/Frameworks" "$bundle/Frameworks"
clang -arch arm64 -arch x86_64 -mmacosx-version-min="$DEPLOYMENT_TARGET" -Wall -Werror -Wno-deprecated-declarations -O1 \
  -I"$OUT/include" "$here/tests/embed_test.c" -o "$bundle/MacOS/embed_test" \
  -L"$OUT/Frameworks" -lmpv.2 -framework OpenGL -Wl,-rpath,@executable_path/../Frameworks
for arch in $arches; do
  logf="$TMP/embed-$arch.log"
  pre=()
  [ "$arch" = "$HOST_ARCH" ] || pre=(arch "-$arch")
  set +e
  with_timeout 60 ${pre[@]+"${pre[@]}"} env DYLD_PRINT_LIBRARIES=1 "$bundle/MacOS/embed_test" > "$logf" 2>&1
  rc=$?
  set -e
  if [ $rc -ne 0 ] || ! grep -q 'embed_test: basic OK' "$logf" || ! grep -q "^arch=$arch " "$logf"; then
    fail "embed_test（${arch}，退出码 ${rc}）"; show_log "$logf"; continue
  fi
  # 装进来的每一个非系统镜像都必须来自包内的 Frameworks（证明 @rpath 生效，没有误用 runner 上的库）
  stray=$(grep -o 'dyld\[[0-9]*\]: <[^>]*> .*\|dyld\[[0-9]*\]: /.*' "$logf" | sed 's/^dyld\[[0-9]*\]: //; s/^<[^>]*> //' \
    | grep -v -e '^/usr/lib/' -e '^/System/' -e "^$bundle/" || true)
  loaded=$(grep -c "$bundle/Frameworks/" "$logf" || true)
  if [ -n "$stray" ]; then fail "embed_test（${arch}）装载了包外的库：$stray"
  elif [ "$loaded" -lt 5 ]; then fail "embed_test（${arch}）：DYLD_PRINT_LIBRARIES 里只看到 $loaded 个包内库"; show_log "$logf"
  else pass "embed_test（${arch}）：$(grep -o 'ffmpeg-version=[^ ]*' "$logf")，$loaded 个库全部来自 Frameworks/"; fi
done

arch=$HOST_ARCH
for hw in no videotoolbox; do
  logf="$TMP/render-$hw.log"
  set +e
  with_timeout 90 "$bundle/MacOS/embed_test" --render "$MEDIA/h264-aac-ass.mkv" "$hw" > "$logf" 2>&1
  rc=$?
  set -e
  if [ $rc -eq 0 ]; then pass "render API（OpenGL，hwdec=${hw}）：$(grep '^frames=' "$logf")"
  elif [ $rc -eq 77 ]; then soft "render API（hwdec=${hw}）：没有可用的 OpenGL 上下文，跳过"; show_log "$logf" 5
  elif [ "$hw" = no ]; then fail "render API（OpenGL，hwdec=no，退出码 ${rc}）"; show_log "$logf"
  else soft "render API（hwdec=${hw}，退出码 ${rc}）"; show_log "$logf" 10; fi
done

# 命令行 mpv 自己开窗口（cocoa-cb）：浏览器模式靠它。需要图形会话，没有就只警告。
# 本地跑 verify.sh 时屏幕上会闪过一个 2 秒的 mpv 窗口，这是这一项在测

logf="$TMP/window.log"
if mpv_run "$arch" "$logf" --no-config --vo=libmpv --ao=null --hwdec=no --frames=48 -v "$MEDIA/h264-aac-ass.mkv" \
  && grep -q 'VO: \[libmpv\]' "$logf"; then
  pass "命令行 mpv 开窗口播放（cocoa-cb）"
else
  soft "命令行 mpv 开窗口播放（cocoa-cb）"; show_log "$logf" 15
fi

for hw in h264-aac-ass.mkv hevc10-flac.mkv; do
  logf="$TMP/vt-$hw.log"
  if mpv_run "$arch" "$logf" --no-config --vo=null --ao=null --hwdec=videotoolbox-copy --frames=48 "$MEDIA/$hw" \
    && grep -q 'Using hardware decoding (videotoolbox-copy)' "$logf"; then
    pass "VideoToolbox 硬解：$hw"
  else
    soft "VideoToolbox 硬解：$hw"; show_log "$logf" 8
  fi
done

echo
echo "通过 ${passed}，失败 ${failed}，警告 $warned"
if [ $failed -ne 0 ]; then
  printf "失败项：$failures\n" >&2
  exit 1
fi
