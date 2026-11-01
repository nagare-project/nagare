package artcache

import (
	"image"
	"math"

	"github.com/nagare-project/nagare/internal/colorspace"
)

// 从一张解码好的封面里取「主色相」。
//
// 算法沿用作者另一个项目的取色实现：把代表色换成 OKLCH，彩度低于 0.005 的视为没有可信色相
// （灰的封面不编一个颜色出来），否则取 OKLab 的 atan2(b, a) 作为色相。那边的代表色由上游元数据给出，
// nagare 没有这个字段，所以先在本机从封面像素里找出代表色：
//
//  1. 在封面上均匀取至多 toneGrid × toneGrid 个采样点（取格子中心，不读整张图的每个像素）；
//     几乎透明的点跳过。
//  2. 每个点换成 OKLab。彩度不到 minSampleChroma 的点（灰、黑、白、JPEG 压缩噪点）不参与投票。
//  3. 其余的点按色相落进 36 个 10° 的格子，票数 = 彩度：面积大、颜色浓的那一簇赢。
//     找格子时把左右邻格各算半票，免得一簇颜色正好压在格子边界上被劈成两半。
//  4. 判灰：全部采样点的平均彩度（灰点按 0 计）低于 0.005（上面那个阈值）→ 没有色相。
//     黑白封面上一小块红字不够格，占到几个百分点的面积才算这张封面的颜色。
//  5. 代表色 = 赢的那格连同左右邻格里所有点的 OKLab 之和（每个点的 (a, b) 本身就是按彩度加权的方向），
//     色相 = atan2(Σb, Σa)。窗口只有 30°，不存在环形平均的跨 0° 问题。

const (
	// toneGrid 是每条边最多的采样点数：96 × 96 ≈ 九千个点，对一张封面的色相已经足够稳定。
	toneGrid = 96
	// minSampleChroma 以下的像素不投票：灰阶与压缩噪点的色相没有意义。
	minSampleChroma = 0.02
	// grayscaleChroma 是判灰阈值（沿用参考实现的 0.005）。
	grayscaleChroma = 0.005
	// hueBins 是色相直方图的格数（每格 10°）。
	hueBins = 36
	// opaqueAlpha 以下（16 位 alpha 的一半）算透明，不采样。
	opaqueAlpha = 0x8000
)

type hueBin struct{ weight, a, b float64 }

// dominantHue 返回封面的主色相（度，0–360）；ok = false 表示封面是灰的，或几乎全透明。
func dominantHue(img image.Image) (hue float64, ok bool) {
	bounds := img.Bounds()
	w, h := int64(bounds.Dx()), int64(bounds.Dy())
	if w <= 0 || h <= 0 {
		return 0, false
	}
	nx, ny := min(w, toneGrid), min(h, toneGrid)

	var bins [hueBins]hueBin
	var samples int
	var chromaSum float64
	for j := int64(0); j < ny; j++ {
		y := bounds.Min.Y + int((2*j+1)*h/(2*ny))
		for i := int64(0); i < nx; i++ {
			x := bounds.Min.X + int((2*i+1)*w/(2*nx))
			lab, opaque := samplePixel(img, x, y)
			if !opaque {
				continue
			}
			samples++
			c := math.Hypot(lab.A, lab.B)
			if c < minSampleChroma {
				continue
			}
			chromaSum += c
			k := int(lab.LCH().H/(360/hueBins)) % hueBins
			bins[k].weight += c
			bins[k].a += lab.A
			bins[k].b += lab.B
		}
	}
	if samples == 0 || chromaSum/float64(samples) < grayscaleChroma {
		return 0, false
	}

	best, bestWeight := 0, -1.0
	for k := range bins {
		prev, next := bins[(k+hueBins-1)%hueBins], bins[(k+1)%hueBins]
		if weight := bins[k].weight + (prev.weight+next.weight)/2; weight > bestWeight {
			best, bestWeight = k, weight
		}
	}
	var sum colorspace.OKLab
	for d := -1; d <= 1; d++ {
		bin := bins[(best+d+hueBins)%hueBins]
		sum.A += bin.a
		sum.B += bin.b
	}
	return sum.LCH().H, true
}

// samplePixel 取一个像素的 OKLab；opaque = false 表示这个点几乎透明，不该参与。
func samplePixel(img image.Image, x, y int) (lab colorspace.OKLab, opaque bool) {
	r, g, b, a := img.At(x, y).RGBA() // 16 位、预乘 alpha
	if a < opaqueAlpha {
		return colorspace.OKLab{}, false
	}
	unpremultiply := func(v uint32) float64 { return float64(v) / float64(a) }
	return colorspace.OKLabFromRGB(colorspace.RGB{R: unpremultiply(r), G: unpremultiply(g), B: unpremultiply(b)}), true
}
