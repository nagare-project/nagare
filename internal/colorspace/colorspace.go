// Package colorspace 是 nagare 用到的颜色数学：sRGB 的编码与线性光、OKLab / OKLCH、
// 向 sRGB 的色域映射（CSS Color 4 的降彩度做法），以及 WCAG 2.x 的相对亮度与对比度。
//
// 两个使用方共用这一份：设计 token 生成器（tools/gentokens，算中性色阶与对比度表）
// 与封面取色（internal/artcache，把封面色相换成最终下发的 sRGB 值）。
// 公式两边各写一份，迟早有一边改了另一边没改，界面上的颜色与文档里的对比度就对不上了。
//
// 只用标准库。OKLab 的矩阵来自 Björn Ottosson 的原始定义（https://bottosson.github.io/posts/oklab/），
// 与 CSS Color 4 规范里的 OKLab 一致。
package colorspace

import (
	"fmt"
	"math"
)

// LinearFromSRGB 把一个 sRGB 编码值（0–1）换成线性光（IEC 61966-2-1 的分段曲线）。
func LinearFromSRGB(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// SRGBFromLinear 是 LinearFromSRGB 的反函数：线性光（0–1）→ sRGB 编码值。
// 负数按原样走线性段，调用方负责裁剪；色域映射要靠「出没出界」来判断，这里不能先夹住。
func SRGBFromLinear(v float64) float64 {
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// RGB 是 sRGB 编码后的颜色，三个通道都是 0–1（色域映射之前可能越界）。
type RGB struct{ R, G, B float64 }

// RGBFrom8 从 0–255 的通道构造。
func RGBFrom8(r, g, b uint8) RGB {
	return RGB{float64(r) / 255, float64(g) / 255, float64(b) / 255}
}

// Bytes 四舍五入到 0–255，越界的通道先夹到边上。
func (c RGB) Bytes() (r, g, b uint8) {
	q := func(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255)) }
	return q(c.R), q(c.G), q(c.B)
}

// Hex 是小写的 #rrggbb。
func (c RGB) Hex() string {
	r, g, b := c.Bytes()
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// Quantize 是实际显示的颜色：先取 8 位（Hex 写出去的就是它），再换回 0–1。
// 对比度要按这个算，而不是按色域映射出来的小数 —— 用户看到的是取整之后的那个。
func (c RGB) Quantize() RGB {
	r, g, b := c.Bytes()
	return RGBFrom8(r, g, b)
}

// inGamut 判断是否在 sRGB 色域里；容差吸收浮点误差（中性灰的三个通道算出来会差 1e-16）。
func (c RGB) inGamut() bool {
	const eps = 1e-9
	in := func(v float64) bool { return v >= -eps && v <= 1+eps }
	return in(c.R) && in(c.G) && in(c.B)
}

// clip 把每个通道夹进 [0, 1]：CSS Color 4 色域映射里的「clip」。
func (c RGB) clip() RGB {
	f := func(v float64) float64 { return math.Max(0, math.Min(1, v)) }
	return RGB{f(c.R), f(c.G), f(c.B)}
}

// OKLab 是 Ottosson 的 OKLab：L 是明度（0–1），A / B 是两个对立色轴。
type OKLab struct{ L, A, B float64 }

// OKLCH 是 OKLab 的极坐标形式：C 是彩度，H 是色相角（度，0–360）。
type OKLCH struct{ L, C, H float64 }

// OKLabFromLinear 把线性光 sRGB 换成 OKLab。
func OKLabFromLinear(r, g, b float64) OKLab {
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return OKLab{
		L: 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		A: 1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		B: 0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

// OKLabFromRGB 把 sRGB 编码值换成 OKLab。
func OKLabFromRGB(c RGB) OKLab {
	return OKLabFromLinear(LinearFromSRGB(c.R), LinearFromSRGB(c.G), LinearFromSRGB(c.B))
}

// Linear 换回线性光 sRGB（可能越界：OKLab 能表示 sRGB 之外的颜色）。
func (o OKLab) Linear() (r, g, b float64) {
	l := o.L + 0.3963377774*o.A + 0.2158037573*o.B
	m := o.L - 0.1055613458*o.A - 0.0638541728*o.B
	s := o.L - 0.0894841775*o.A - 1.2914855480*o.B
	l, m, s = l*l*l, m*m*m, s*s*s
	return 4.0767416621*l - 3.3077115913*m + 0.2309699292*s,
		-1.2684380046*l + 2.6097574011*m - 0.3413193965*s,
		-0.0041960863*l - 0.7034186147*m + 1.7076147010*s
}

// RGB 换成 sRGB 编码值（不做色域映射，可能越界）。
func (o OKLab) RGB() RGB {
	r, g, b := o.Linear()
	return RGB{SRGBFromLinear(r), SRGBFromLinear(g), SRGBFromLinear(b)}
}

// LCH 换成极坐标；色相规整到 [0, 360)。
func (o OKLab) LCH() OKLCH {
	h := math.Atan2(o.B, o.A) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return OKLCH{L: o.L, C: math.Hypot(o.A, o.B), H: h}
}

// Lab 换回直角坐标。
func (c OKLCH) Lab() OKLab {
	rad := c.H * math.Pi / 180
	return OKLab{L: c.L, A: c.C * math.Cos(rad), B: c.C * math.Sin(rad)}
}

// Neutral 是彩度为 0 的 OKLCH 颜色：OKLab 的 a = b = 0 时，线性 sRGB 三个通道都等于 L³
// （逆变换矩阵每行之和为 1），再做 sRGB 编码。海报没有色相时界面用的就是这一档中性值。
func Neutral(l float64) RGB {
	v := SRGBFromLinear(l * l * l)
	return RGB{v, v, v}
}
