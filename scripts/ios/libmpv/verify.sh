#!/usr/bin/env bash
# 验证 package.sh xcframework 产出的那一套文件。任何一项失败都以非零退出。
#
# 用法：verify.sh <产物目录> [--simulator]
#
# 静态（不需要模拟器）：
#   * Libmpv.xcframework：三个切片齐全（真机 arm64、模拟器 arm64 + x86_64）、平台对、minos ≤ 15.0、
#     只依赖系统库、只导出 mpv_*、Info.plist / 头文件 / module map / 签名（tools/xcf.py check）
#   * 许可证：整套 LGPL-2.1-or-later，没有任何 GPL 组件 ——
#       FFmpeg：configure 的结论是「LGPL version 2.1 or later」，CONFIG_GPL / VERSION3 / NONFREE 都是 0；
#       mpv：HAVE_GPL 0，Copyright 列的「GPL only」文件一个都没编进去；
#       其余组件：license-choices.txt 逐项对 versions.lock，随包的许可证原文里没有 GPL 的
#   * 不带 TLS（没有 https / tls / crypto 协议），VideoToolbox 硬解与 render API 要的组件都在，
#     LuaJIT 的 JIT 编译器没编进去（LJ_HASJIT 0）
#   * iOS 的 FFmpeg 解码 / 分离 / 解析器清单是 macOS 版的超集
# 运行（--simulator：建一台 iOS 模拟器，用本机架构对应的模拟器切片跑 tests/sim_test.m）：
#   初始化与版本 / 媒体库扫描器认的每个扩展名各解一段 / 回环地址 http 能播、https 被拒 /
#   Lua（LuaJIT、jit.status() 为 false、jit.on() 被拒）/ render API 在 OpenGL ES 上画帧（软解，
#   VideoToolbox 为可选项）
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
mac="$here/../../macos/libmpv"
# shellcheck source=../../macos/libmpv/lib/env.sh
. "$mac/lib/env.sh"
# shellcheck source=lib/env.sh
. "$here/lib/env.sh"
# shellcheck source=../../macos/libmpv/lib/sources.sh
. "$mac/lib/sources.sh"
# 配方只为拿到 iOS 的 DEP_ORDER 与各组件的源码清单（ios_used_sources）
# shellcheck source=../../macos/libmpv/lib/recipes.sh
. "$mac/lib/recipes.sh"
# shellcheck source=lib/recipes.sh
. "$here/lib/recipes.sh"

[ $# -ge 1 ] || die "用法：verify.sh <产物目录> [--simulator]"
OUT=$(cd "$1" && pwd)
simulator=false
[ "${2:-}" = --simulator ] && simulator=true
MEDIA="$mac/tests/media"
XCF="$OUT/Libmpv.xcframework"

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
SIM_UDID=""
cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  if [ -n "$SIM_UDID" ]; then
    xcrun simctl shutdown "$SIM_UDID" >/dev/null 2>&1 || true
    xcrun simctl delete "$SIM_UDID" >/dev/null 2>&1 || true
  fi
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
show_log() { sed 's/^/      | /' "$1" | tail -n "${2:-25}"; }
with_timeout() {
  local secs=$1
  shift
  perl -e 'alarm shift; exec @ARGV or die "exec: $!"' "$secs" "$@"
}
py() { python3 -I -B "$here/tools/xcf.py" "$@"; }

want_mpv=$(lock_field mpv 2)

# ---------------------------------------------------------------------------
echo "== xcframework 静态检查 =="
py check "$OUT" --minos "$DEPLOYMENT_TARGET" --version "$want_mpv" || fail "xcframework 静态检查"

echo "== FFmpeg =="
# 必须能用的（与 macOS 版相同，只是协议换成 iOS 版的 file / http / tcp）
REQUIRED_FF="
h264_decoder hevc_decoder libdav1d_decoder av1_decoder vp9_decoder vp8_decoder
aac_decoder flac_decoder opus_decoder vorbis_decoder ac3_decoder eac3_decoder truehd_decoder dca_decoder
mp3_decoder mp3float_decoder pcm_s16le_decoder pcm_s24le_decoder pcm_bluray_decoder
ass_decoder subrip_decoder webvtt_decoder pgssub_decoder movtext_decoder
matroska_demuxer mov_demuxer mpegts_demuxer hls_demuxer ass_demuxer srt_demuxer webvtt_demuxer sup_demuxer
h264_videotoolbox_hwaccel hevc_videotoolbox_hwaccel vp9_videotoolbox_hwaccel av1_videotoolbox_hwaccel
mjpeg_encoder png_encoder rawvideo_encoder image2_muxer
file_protocol http_protocol tcp_protocol
scale_filter crop_filter pad_filter format_filter hflip_filter vflip_filter zscale_filter tonemap_filter
avi_demuxer flv_demuxer asf_demuxer rm_demuxer m4v_demuxer
mpeg4_decoder msmpeg4v1_decoder msmpeg4v2_decoder msmpeg4v3_decoder h263_decoder alac_decoder
wmv1_decoder wmv2_decoder wmv3_decoder vc1_decoder wmav1_decoder wmav2_decoder wmapro_decoder
rv10_decoder rv20_decoder rv30_decoder rv40_decoder ra_144_decoder ra_288_decoder cook_decoder sipr_decoder atrac3_decoder
flv_decoder vp6_decoder vp6f_decoder vp6a_decoder adpcm_swf_decoder nellymoser_decoder adpcm_ms_decoder
"
# 不该有的：GPL 的编码器、TLS（iOS 版不播网络源）、Metal 滤镜
FORBIDDEN_FF="libx264_encoder libx265_encoder h264_videotoolbox_encoder aac_encoder bluray_protocol
https_protocol tls_protocol crypto_protocol tls_securetransport_protocol yadif_videotoolbox_filter"
for t in $IOS_TARGETS; do
  info="$OUT/info/$t"
  comps="$info/ffmpeg-components.txt"
  if [ ! -s "$comps" ]; then fail "缺少 info/$t/ffmpeg-components.txt"; continue; fi
  missing=""
  for c in $REQUIRED_FF; do grep -qx "$c" "$comps" || missing="$missing $c"; done
  [ -z "$missing" ] && pass "FFmpeg 组件齐全（${t}，$(wc -l < "$comps" | tr -d ' ') 个）" || fail "FFmpeg（${t}）缺少：$missing"
  extra=""
  for c in $FORBIDDEN_FF; do grep -qx "$c" "$comps" && extra="$extra $c"; done
  [ -z "$extra" ] && pass "FFmpeg 没有 TLS 与多余的编码器（${t}）" || fail "FFmpeg（${t}）不该有：$extra"
  summary=$(grep '^License:' "$info/ffmpeg-configure-summary.txt" || true)
  lic="$info/ffmpeg-license.txt"
  if [ "$summary" = "License: LGPL version 2.1 or later" ] && grep -qx '#define CONFIG_GPL 0' "$lic" \
    && grep -qx '#define CONFIG_VERSION3 0' "$lic" && grep -qx '#define CONFIG_NONFREE 0' "$lic" \
    && grep -qx '#define FFMPEG_LICENSE "LGPL version 2.1 or later"' "$lic"; then
    pass "FFmpeg 许可证 LGPL v2.1+（${t}）"
  else
    fail "FFmpeg（${t}）不是 LGPL v2.1+：${summary}；$(tr '\n' ' ' < "$lic")"
  fi
  bad_args=$(grep -xE -e '--enable-(gpl|version3|nonfree|mbedtls|gnutls|openssl|securetransport)' \
    "$info/ffmpeg-configure-args.txt" || true)
  [ -z "$bad_args" ] && pass "FFmpeg 配置参数里没有 GPL / v3 / TLS 开关（${t}）" || fail "FFmpeg（${t}）配置参数：$bad_args"
done
py ff-superset "$mac/lib/recipes.sh" "$here/lib/recipes.sh" || fail "iOS 的 FFmpeg 清单少于 macOS 版"

echo "== mpv =="
REQUIRED_MPV_FEATURES="luajit libass libplacebo gl ios-gl audiounit lcms2 uchardet jpeg zlib zimg iconv"
FORBIDDEN_MPV_FEATURES="gpl cocoa swift gl-cocoa videotoolbox-gl macos-cocoa-cb coreaudio avfoundation cplugins
javascript vulkan libbluray libarchive rubberband sdl2-video dvdnav cdda"
for t in $IOS_TARGETS; do
  info="$OUT/info/$t"
  feats=" $(tr -d '"' < "$info/mpv-features.txt") "
  missing=""
  for f in $REQUIRED_MPV_FEATURES; do case "$feats" in *" $f "*) ;; *) missing="$missing $f" ;; esac; done
  [ -z "$missing" ] && pass "mpv 特性齐全（${t}）" || fail "mpv（${t}）缺少特性：$missing"
  extra=""
  for f in $FORBIDDEN_MPV_FEATURES; do case "$feats" in *" $f "*) extra="$extra $f" ;; esac; done
  [ -z "$extra" ] && pass "mpv 没有 GPL / macOS 专属特性（${t}）" || fail "mpv（${t}）不该开：$extra"
  if grep -qx '#define HAVE_GPL 0' "$info/mpv-config.txt"; then pass "mpv HAVE_GPL 0（${t}）"
  else fail "mpv（${t}）不是 -Dgpl=false：$(tr '\n' ' ' < "$info/mpv-config.txt")"; fi
  py gpl-files "$OUT/licenses/mpv/Copyright" "$info/mpv-sources.txt" || fail "mpv（${t}）编进了 GPL only 的文件"
done

echo "== LuaJIT =="
for t in $IOS_TARGETS; do
  jitf="$OUT/info/$t/luajit-jit.txt"
  if grep -qx '#define LJ_HASJIT 0' "$jitf" 2>/dev/null && grep -qx '#define LJ_TARGET_IOS 1' "$jitf"; then
    pass "LuaJIT 没有 JIT 编译器（${t}：LJ_HASJIT 0，LJ_TARGET_IOS 1）"
  else
    fail "LuaJIT（${t}）：$(tr '\n' ' ' < "$jitf" 2>/dev/null || echo 缺少 luajit-jit.txt)"
  fi
done

echo "== 组件许可证 =="
ios_used_sources > "$TMP/used-sources.txt"
py licenses --lock "$LOCK_FILE" --choices "$here/license-choices.txt" --used "$TMP/used-sources.txt" \
  || fail "组件许可证"
deps=$DEP_ORDER
for d in $deps; do
  [ -d "$OUT/licenses/$d" ] && [ -n "$(ls "$OUT/licenses/$d")" ] || fail "缺少 licenses/$d/"
done
extra_dirs=""
for d in "$OUT"/licenses/*; do
  case " $deps " in *" $(basename "$d") "*) ;; *) extra_dirs="$extra_dirs $(basename "$d")" ;; esac
done
[ -z "$extra_dirs" ] || fail "licenses/ 里有这次没用到的组件：$extra_dirs"
gpl_texts=$(cd "$OUT/licenses" && find . -type f | grep -E '(^|[^L])GPL' || true)
[ -z "$gpl_texts" ] && pass "随包的许可证原文里没有 GPL（$(cd "$OUT/licenses" && find . -type f | wc -l | tr -d ' ') 个文件）" \
  || fail "随包带了 GPL 的许可证原文：$(echo $gpl_texts)"

if ! $simulator; then
  echo
  echo "通过 ${passed}，失败 ${failed}，警告 $warned（没有跑模拟器：加 --simulator）"
  [ $failed -eq 0 ] || { printf '失败项：%b\n' "$failures" >&2; exit 1; }
  exit 0
fi

# ---------------------------------------------------------------------------
echo "== 模拟器 =="
sim_arch=$HOST_ARCH
sim_lib="$XCF/ios-arm64_x86_64-simulator"
bin="$TMP/sim_test"
xcrun --sdk iphonesimulator clang -target "$sim_arch-apple-ios$DEPLOYMENT_TARGET-simulator" \
  -isysroot "$(xcrun --sdk iphonesimulator --show-sdk-path)" -fobjc-arc -Wall -Werror -Wno-deprecated-declarations -O1 \
  -F "$sim_lib" -framework Libmpv -framework OpenGLES -framework Foundation -Wl,-rpath,"$sim_lib" \
  "$here/tests/sim_test.m" -o "$bin" || die "编不出 sim_test"

runtime=$(xcrun simctl list runtimes available -j | python3 -I -B -c '
import json, sys
rts = [r for r in json.load(sys.stdin)["runtimes"] if r.get("platform") == "iOS" and r.get("isAvailable")]
rts.sort(key=lambda r: tuple(int(x) for x in r["version"].split(".")))
print(rts[-1]["identifier"] if rts else "")')
[ -n "$runtime" ] || die "没有可用的 iOS 模拟器运行时（xcrun simctl list runtimes）"
devtype=$(xcrun simctl list devicetypes -j | python3 -I -B -c '
import json, sys
names = [d["identifier"] for d in json.load(sys.stdin)["devicetypes"] if d.get("productFamily") == "iPhone"]
preferred = "com.apple.CoreSimulator.SimDeviceType.iPhone-16"
print(preferred if preferred in names else (names[-1] if names else ""))')
[ -n "$devtype" ] || die "没有可用的 iPhone 模拟器机型"
SIM_UDID=$(xcrun simctl create nagare-libmpv-verify "$devtype" "$runtime")
log "模拟器：${SIM_UDID}（${devtype}，${runtime}）"
xcrun simctl boot "$SIM_UDID"
with_timeout 300 xcrun simctl bootstatus "$SIM_UDID" -b >/dev/null || die "模拟器 5 分钟内没启动起来"

# sim <日志名> 参数...：在模拟器里跑 sim_test，输出进日志，返回它的退出码
sim() {
  local logf="$TMP/sim-$1.log"
  shift
  set +e
  with_timeout 120 xcrun simctl spawn "$SIM_UDID" "$bin" "$@" > "$logf" 2>&1
  local rc=$?
  set -e
  return $rc
}

if sim info info && grep -q "^arch=$sim_arch mpv-version=mpv v$want_mpv" "$TMP/sim-info.log"; then
  pass "模拟器里初始化：$(head -1 "$TMP/sim-info.log")"
else
  fail "模拟器里初始化（${sim_arch}）"; show_log "$TMP/sim-info.log"
fi
protocols=$(sed -n 's/^protocol-list=//p' "$TMP/sim-info.log")
case ",$protocols," in
  *,https,* | *,tls,*) fail "协议清单里有 https / tls：$protocols" ;;
  *,http,*) pass "协议：${protocols}（有 http，没有 https / tls）" ;;
  *) fail "协议清单里没有 http：$protocols" ;;
esac

# 媒体库扫描器认的每个扩展名（与 macOS 的 verify.sh 同一张表）+ 几种主要编码
for f in h264-aac-ass.mkv h264-eac3-movtext.mp4 mpeg4-mp3.avi vp9-vorbis.webm h264-alac.mov mpeg4-aac.m4v \
  flv1-adpcm.flv wmv2-wmav2.wmv mpeg2-ac3.ts rv20-ra144.rmvb hevc10-flac.mkv av1-opus.webm; do
  if sim "play-$f" play "$MEDIA/$f"; then pass "模拟器里解码 ${f}：$(grep -o 'width=[0-9]* samplerate=[0-9]*' "$TMP/sim-play-$f.log")"
  else fail "模拟器里解码 $f"; show_log "$TMP/sim-play-$f.log" 15; fi
done

python3 -I -B "$here/tests/http_server.py" "$MEDIA" "$TMP/port" > "$TMP/http-server.log" 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 600); do [ -s "$TMP/port" ] && break; sleep 0.1; done
[ -s "$TMP/port" ] || die "HTTP 测试服务端 60 秒内没起来：$(cat "$TMP/http-server.log")"
port=$(cat "$TMP/port")
if sim http play "http://127.0.0.1:$port/h264-aac-ass.mkv"; then pass "模拟器里经回环地址 http 播放"
else fail "模拟器里经回环地址 http 播放"; show_log "$TMP/sim-http.log" 15; fi
rc=0
sim https play "https://127.0.0.1:$port/h264-aac-ass.mkv" || rc=$?
if [ $rc -eq 3 ]; then pass "https:// 被拒（没有 TLS）"
else fail "https:// 应当因为没有协议而失败，实际退出码 $rc"; show_log "$TMP/sim-https.log" 10; fi

if sim lua lua "$here/tests/lua_check.lua" "$MEDIA/h264-aac-ass.mkv" \
  && grep -q "lua-check engine=LuaJIT_2\.1[^ ]* arch=$( [ "$sim_arch" = x86_64 ] && echo x64 || echo arm64) jit=off jit-on=refused " \
    "$TMP/sim-lua.log"; then
  pass "Lua：$(grep -o 'lua-check engine=[^ ]* arch=[^ ]* jit=[^ ]* jit-on=[^ ]*' "$TMP/sim-lua.log")"
else
  fail "Lua（LuaJIT、关 JIT）"; show_log "$TMP/sim-lua.log"
fi

for hw in no videotoolbox; do
  rc=0
  sim "render-$hw" render "$MEDIA/h264-aac-ass.mkv" "$hw" || rc=$?
  label="render API（OpenGL ES，hwdec=${hw}）"
  if [ $rc -eq 0 ]; then pass "${label}：$(grep '^GL_RENDERER' "$TMP/sim-render-$hw.log")；$(grep '^frames=' "$TMP/sim-render-$hw.log")"
  elif [ "$hw" = no ]; then fail "${label}，退出码 ${rc}"; show_log "$TMP/sim-render-$hw.log"
  else soft "${label}，退出码 ${rc}（模拟器的 VideoToolbox 不一定能交给 GL）"; show_log "$TMP/sim-render-$hw.log" 10; fi
done

echo
echo "通过 ${passed}，失败 ${failed}，警告 $warned"
if [ $failed -ne 0 ]; then
  printf '失败项：%b\n' "$failures" >&2
  exit 1
fi
