package api

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/nagare-project/nagare/internal/colorspace"
)

// 发现页的颜色：目录给每部作品带一个封面主色（posterAccent，#rrggbb），发现页只取它的 OKLCH 色相角，
// 明度与彩度由前端按固定的几档拼成 oklch()（frontend/src/lib/home/tone.ts）。所以无论封面是什么颜色，
// 每一档的对比度都有下界 —— 与作品页的海报色阶（posterTones）同一个思路，只是色相的来源不同：
// posterTones 要等本机把封面缓存下来再采样，发现页第一屏就要上色，用的是目录现成给的那个颜色。

// accentPlaceholder 是上游在取不到可用封面色时填的占位色。它不是这部作品的颜色：
// 约 7% 的作品带着它，当成紫色的话，一堆毫不相干的作品会被归进同一个色调。
const accentPlaceholder = "#8b5cf6"

// accentGreyChroma 以下算灰色：彩度接近 0 时 atan2 的角度只是舍入噪声，换一次取整就从红跳到蓝。
const accentGreyChroma = 0.04

var accentHex = regexp.MustCompile(`^#?([0-9a-fA-F]{6})$`)

// accentHue 是封面主色的 OKLCH 色相角（度，[0, 360)，保留一位小数）；nil = 没有可信色相，界面用中性色。
// 占位色、格式不对、灰色都是 nil。
func accentHue(posterAccent string) *float64 {
	raw := strings.TrimSpace(posterAccent)
	m := accentHex.FindStringSubmatch(raw)
	if m == nil || strings.EqualFold("#"+m[1], accentPlaceholder) {
		return nil
	}
	n, err := strconv.ParseUint(m[1], 16, 32)
	if err != nil {
		return nil
	}
	lch := colorspace.OKLabFromRGB(colorspace.RGBFrom8(uint8(n>>16), uint8(n>>8), uint8(n))).LCH()
	if lch.C < accentGreyChroma {
		return nil
	}
	hue := math.Round(lch.H*10) / 10
	if hue >= 360 {
		hue -= 360
	}
	return &hue
}
