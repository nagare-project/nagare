package colorspace

// RelativeLuminance 是 WCAG 2.x 的相对亮度；参数是 sRGB 编码值（0–1，可以带小数，
// 半透明颜色叠到底色上之后的通道就不是整数）。
func RelativeLuminance(r, g, b float64) float64 {
	return 0.2126*LinearFromSRGB(r) + 0.7152*LinearFromSRGB(g) + 0.0722*LinearFromSRGB(b)
}

// Luminance 是 c 的 WCAG 相对亮度。
func (c RGB) Luminance() float64 { return RelativeLuminance(c.R, c.G, c.B) }

// ContrastRatio 是两个相对亮度之间的 WCAG 对比度（顺序无关，1–21）。
func ContrastRatio(l1, l2 float64) float64 {
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// Contrast 是两个不透明颜色之间的 WCAG 对比度。
func Contrast(a, b RGB) float64 { return ContrastRatio(a.Luminance(), b.Luminance()) }
