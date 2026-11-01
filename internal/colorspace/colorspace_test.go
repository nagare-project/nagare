package colorspace

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// sRGB 三原色的 OKLCH 是公开的参照值（CSS Color 4 与 Ottosson 原文都能对上）。
func TestOKLabReferenceValues(t *testing.T) {
	cases := []struct {
		name    string
		rgb     RGB
		l, c, h float64
	}{
		{"红", RGBFrom8(255, 0, 0), 0.62796, 0.25768, 29.234},
		{"绿", RGBFrom8(0, 255, 0), 0.86644, 0.29483, 142.495},
		{"蓝", RGBFrom8(0, 0, 255), 0.45201, 0.31321, 264.052},
		{"白", RGBFrom8(255, 255, 255), 1, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := OKLabFromRGB(tc.rgb).LCH()
			assert.InDelta(t, tc.l, got.L, 1e-4)
			assert.InDelta(t, tc.c, got.C, 1e-4)
			if tc.c > 0 {
				assert.InDelta(t, tc.h, got.H, 0.01)
			}
		})
	}
}

// sRGB → OKLab → sRGB 往返不丢精度。
func TestRoundTrip(t *testing.T) {
	for _, c := range []RGB{RGBFrom8(12, 200, 99), RGBFrom8(250, 3, 130), RGBFrom8(1, 1, 1), RGBFrom8(128, 128, 128)} {
		back := OKLabFromRGB(c).LCH().Lab().RGB()
		assert.InDelta(t, c.R, back.R, 1e-6)
		assert.InDelta(t, c.G, back.G, 1e-6)
		assert.InDelta(t, c.B, back.B, 1e-6)
	}
}

func TestNeutral(t *testing.T) {
	assert.Equal(t, "#b1b1b1", Neutral(0.76).Hex())
	assert.Equal(t, "#000000", Neutral(0).Hex())
	assert.Equal(t, "#ffffff", Neutral(1).Hex())
	// 彩度为 0 时，完整的 OKLCH → sRGB 路径与 Neutral 的捷径得到同一个颜色
	for _, l := range []float64{0.15, 0.24, 0.52, 0.68, 0.76} {
		assert.Equal(t, Neutral(l).Hex(), ToSRGB(OKLCH{L: l, C: 0, H: 123}).Hex(), "L=%v", l)
	}
}

func TestToSRGBKeepsInGamutColours(t *testing.T) {
	// 本来就在色域里的颜色原样返回
	in := OKLabFromRGB(RGBFrom8(59, 130, 246)).LCH()
	assert.Equal(t, "#3b82f6", ToSRGB(in).Hex())
	assert.Equal(t, "#ffffff", ToSRGB(OKLCH{L: 1.2, C: 0.3, H: 40}).Hex())
	assert.Equal(t, "#000000", ToSRGB(OKLCH{L: -0.1, C: 0.3, H: 40}).Hex())
}

// 色域外的颜色：结果落在色域内，明度与色相基本不动，彩度降下来，与原色的差距约一个 JND。
func TestToSRGBReducesChroma(t *testing.T) {
	for h := 0.0; h < 360; h += 7.5 {
		origin := OKLCH{L: 0.7, C: 0.37, H: h} // 0.37 对任何色相都在 sRGB 之外
		got := ToSRGB(origin)
		assert.True(t, got.inGamut(), "h=%v", h)
		lch := OKLabFromRGB(got).LCH()
		assert.Less(t, lch.C, origin.C, "h=%v 彩度应当降低", h)
		// 终点允许一次轻微裁剪：离「原明度、原色相、降过的彩度」那一点不超过一个 JND
		onLine := OKLCH{L: origin.L, C: lch.C, H: origin.H}.Lab()
		assert.LessOrEqual(t, deltaEOK(got, onLine), gamutJND+1e-3, "h=%v 明度与色相不该被裁剪拉走", h)
		assert.InDelta(t, origin.L, lch.L, gamutJND, "h=%v", h)
	}
}

// 与逐通道裁剪比：饱和的蓝直接裁剪会明显偏离原明度，降彩度不会。
func TestToSRGBBeatsNaiveClip(t *testing.T) {
	origin := OKLCH{L: 0.6, C: 0.3, H: 264}
	naive := OKLabFromRGB(origin.Lab().RGB().clip()).LCH()
	mapped := OKLabFromRGB(ToSRGB(origin)).LCH()
	assert.Less(t, math.Abs(mapped.L-origin.L), math.Abs(naive.L-origin.L))
}

func TestContrast(t *testing.T) {
	white, black := RGBFrom8(255, 255, 255), RGBFrom8(0, 0, 0)
	assert.InDelta(t, 21, Contrast(white, black), 1e-9)
	assert.InDelta(t, 21, Contrast(black, white), 1e-9)
	assert.InDelta(t, 3.65, Contrast(white, RGBFrom8(0x0a, 0x84, 0xff)), 0.005)
}

func TestQuantize(t *testing.T) {
	c := RGB{0.5, 1.2, -0.1}.Quantize()
	assert.Equal(t, RGBFrom8(128, 255, 0), c)
}
