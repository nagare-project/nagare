package artcache

import (
	"math"

	"github.com/nagare-project/nagare/internal/colorspace"
)

// PosterTones 是一部作品的海报色阶，随接口下发（字段 posterTones，取不到色相时为 null）。
//
// 只有色相 Hue 来自封面，明度与彩度固定（design/tokens.json 的 posterTones，DESIGN.md「海报色」一节），
// 所以无论封面是什么颜色，每一档的对比度都有下界。各档已经由服务端做完 sRGB 色域映射，
// 是最终的 #rrggbb：React 与 SwiftUI 直接用，不在客户端算颜色。
type PosterTones struct {
	// Hue 是 OKLCH 色相角（度，0–360，保留一位小数）。给调试与原型用，界面只用下面的 sRGB 值。
	Hue float64 `json:"hue"`
	// Tone 对应 --poster-tone：强调文字、图标、悬停态、主按钮的底。
	Tone string `json:"tone"`
	// Quiet 对应 --poster-tone-quiet：压低一档的小标签，色阶里最后一档能写字的颜色。
	Quiet string `json:"quiet"`
	// Mid 对应 --poster-tone-mid：边框、分隔线，非文字，只在 --bg 上保证 3:1。
	Mid string `json:"mid"`
	// Low 对应 --poster-tone-low：tint 底、chip 填充，纯装饰。
	Low string `json:"low"`
	// OnTone 对应 --poster-on-tone：以 Tone 为底的主按钮上的字。
	OnTone string `json:"onTone"`
}

// toneStep 是色阶的一档。
type toneStep struct {
	name string // tokens.json 里的名字（测试据此核对）
	l, c float64
}

// posterLadder 必须与 design/tokens.json 的 posterTones 一致：ladder_test.go 逐档核对，
// 改了 tokens.json 忘了改这里，测试就红。顺序与 PosterTones 的字段一一对应。
var posterLadder = [5]toneStep{
	{"poster-tone", 0.76, 0.085},
	{"poster-tone-quiet", 0.68, 0.07},
	{"poster-tone-mid", 0.52, 0.07},
	{"poster-tone-low", 0.24, 0.045},
	{"poster-on-tone", 0.15, 0.03},
}

// LadderFor 按色相算出整条色阶。色相先取一位小数：同一张封面每次得到逐字节相同的结果。
func LadderFor(hue float64) PosterTones {
	hue = roundHue(hue)
	var hex [len(posterLadder)]string
	for i, s := range posterLadder {
		hex[i] = colorspace.ToSRGB(colorspace.OKLCH{L: s.l, C: s.c, H: hue}).Hex()
	}
	return PosterTones{Hue: hue, Tone: hex[0], Quiet: hex[1], Mid: hex[2], Low: hex[3], OnTone: hex[4]}
}

// roundHue 规整到 [0, 360) 并保留一位小数（359.96 进位成 0，不写成 360）。
func roundHue(h float64) float64 {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	h = math.Round(h*10) / 10
	if h >= 360 {
		h = 0
	}
	return h
}
