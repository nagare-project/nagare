package main

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/nagare-project/nagare/internal/colorspace"
)

// rgba 是 sRGB 的 0–255 通道加 0–1 的不透明度。
type rgba struct {
	R, G, B uint8
	A       float64
}

var (
	hexRE  = regexp.MustCompile(`^#([0-9a-f]{6})$`)
	rgbaRE = regexp.MustCompile(`^rgba\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*([0-9.]+)\s*\)$`)
)

// parseColor 认 #rrggbb（小写）与 rgba(r, g, b, a)。
func parseColor(s string) (rgba, error) {
	if m := hexRE.FindStringSubmatch(s); m != nil {
		v, _ := strconv.ParseUint(m[1], 16, 32)
		return rgba{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 1}, nil
	}
	m := rgbaRE.FindStringSubmatch(s)
	if m == nil {
		return rgba{}, fmt.Errorf("颜色 %q 要写成 #rrggbb（小写）或 rgba(r, g, b, a)", s)
	}
	var ch [3]uint8
	for i := 0; i < 3; i++ {
		n, _ := strconv.Atoi(m[i+1])
		if n > 255 {
			return rgba{}, fmt.Errorf("颜色 %q 的通道超出 0–255", s)
		}
		ch[i] = uint8(n)
	}
	a, err := strconv.ParseFloat(m[4], 64)
	if err != nil || a < 0 || a > 1 {
		return rgba{}, fmt.Errorf("颜色 %q 的不透明度要在 0 与 1 之间", s)
	}
	return rgba{R: ch[0], G: ch[1], B: ch[2], A: a}, nil
}

// css 是规范写法：不透明写 #rrggbb，半透明写 rgba(r, g, b, a)。
func (c rgba) css() string {
	if c.A == 1 {
		return c.hex()
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", c.R, c.G, c.B, fmtNum(c.A))
}

func (c rgba) hex() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// close 用于核对 iOS 语义色：每个通道差不超过 1、不透明度差不超过 0.005。
func (c rgba) close(o rgba) bool {
	near := func(a, b uint8) bool { return math.Abs(float64(a)-float64(b)) <= 1 }
	return near(c.R, o.R) && near(c.G, o.G) && near(c.B, o.B) && math.Abs(c.A-o.A) <= 0.005
}

// over 把半透明的 c 叠到不透明的 bg 上，得到实际显示的颜色（通道保留小数，算对比度用）。
func (c rgba) over(bg rgba) [3]float64 {
	mix := func(f, b uint8) float64 { return c.A*float64(f) + (1-c.A)*float64(b) }
	return [3]float64{mix(c.R, bg.R), mix(c.G, bg.G), mix(c.B, bg.B)}
}

// luminance 是 WCAG 2.x 的相对亮度；通道是 0–255（叠色之后可以带小数）。
func luminance(ch [3]float64) float64 {
	return colorspace.RelativeLuminance(ch[0]/255, ch[1]/255, ch[2]/255)
}

// contrastRatio 是 fg 叠在不透明的 bg 上之后的 WCAG 对比度。
func contrastRatio(fg, bg rgba) float64 {
	return colorspace.ContrastRatio(luminance(fg.over(bg)), luminance([3]float64{float64(bg.R), float64(bg.G), float64(bg.B)}))
}

// neutralTone 是彩度为 0 时的 OKLCH 颜色换成 sRGB（公式见 colorspace.Neutral）。
// 海报取不到色相（没有封面、封面是灰的）时，色阶就落在这个中性值上。
func neutralTone(l float64) rgba {
	r, g, b := colorspace.Neutral(l).Bytes()
	return rgba{R: r, G: g, B: b, A: 1}
}

// iosDarkColors 是 UIKit 语义色在深色外观（默认对比度）下的取值。nagare 的 iOS 版固定暗色，
// 所以一个 token 只有在它的值与这里一致时才允许映射到系统色 —— 映射之后「增强对比度」
// 由系统接管，而外观仍与网页一致。不在表里的系统色（systemTeal 各版本深色值不同、
// systemFill 一族与网页的填充值不同）一律不映射。
var iosDarkColors = map[string]rgba{
	"systemBackground":          {R: 0x00, G: 0x00, B: 0x00, A: 1},
	"secondarySystemBackground": {R: 0x1c, G: 0x1c, B: 0x1e, A: 1},
	"tertiarySystemBackground":  {R: 0x2c, G: 0x2c, B: 0x2e, A: 1},
	"label":                     {R: 0xff, G: 0xff, B: 0xff, A: 1},
	"secondaryLabel":            {R: 235, G: 235, B: 245, A: 0.6},
	"tertiaryLabel":             {R: 235, G: 235, B: 245, A: 0.3},
	"quaternaryLabel":           {R: 235, G: 235, B: 245, A: 0.18},
	"separator":                 {R: 84, G: 84, B: 88, A: 0.6},
	"opaqueSeparator":           {R: 0x38, G: 0x38, B: 0x3a, A: 1},
	"systemBlue":                {R: 0x0a, G: 0x84, B: 0xff, A: 1},
	"systemGreen":               {R: 0x30, G: 0xd1, B: 0x58, A: 1},
	"systemOrange":              {R: 0xff, G: 0x9f, B: 0x0a, A: 1},
	"systemRed":                 {R: 0xff, G: 0x45, B: 0x3a, A: 1},
}

func iosColorNames() string {
	names := make([]string, 0, len(iosDarkColors))
	for n := range iosDarkColors {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// fmtNum 给出最短的十进制写法：0.6、0.045、48、-0.03。
func fmtNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
