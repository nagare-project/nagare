package colorspace

import "math"

// 色域映射：把一个 OKLCH 颜色放进 sRGB。
//
// 照 CSS Color 4「Gamut Mapping」一节的二分查找算法（Binary Search Gamut Mapping with Local MINDE，
// https://www.w3.org/TR/css-color-4/#binsearch）：明度与色相不动，在 OKLCH 里二分查找彩度，
// 找到「降到这里之后再逐通道裁剪，与未裁剪的颜色差不到 JND」的最大彩度，返回那个裁剪结果。
//
// 为什么不直接逐通道裁剪：裁剪会同时改变明度和色相（饱和的蓝裁完会偏紫、变暗），
// 而海报色阶的可读性全靠固定的明度撑着。降彩度保住明度，色相也基本不动。
// 返回值离原色最多差一个 JND（ΔEOK 0.02）：规范允许终点轻微裁剪，以免为了肉眼看不出的差别
// 把彩度降得太狠。对比度测试按取整后的最终 sRGB 值算，这点偏差已经算进去了。

const (
	// gamutJND 是 OKLab 里「刚好能看出差别」的距离（规范取 0.02）。
	gamutJND = 0.02
	// gamutEpsilon 是二分查找的停止精度（规范取 0.0001）。
	gamutEpsilon = 0.0001
)

// ToSRGB 把 OKLCH 颜色映射进 sRGB 色域，返回 sRGB 编码值（每个通道都在 0–1 内）。
func ToSRGB(c OKLCH) RGB {
	if c.L >= 1 {
		return RGB{1, 1, 1}
	}
	if c.L <= 0 {
		return RGB{0, 0, 0}
	}
	origin := c.Lab().RGB()
	if origin.inGamut() {
		return origin.clip() // 只吸收浮点误差，不改变颜色
	}

	current := c
	clipped := origin.clip()
	if deltaEOK(clipped, c.Lab()) < gamutJND {
		return clipped
	}
	lo, hi := 0.0, c.C
	loInGamut := true
	for hi-lo > gamutEpsilon {
		current.C = (lo + hi) / 2
		candidate := current.Lab().RGB()
		if loInGamut && candidate.inGamut() {
			lo = current.C
			continue
		}
		clipped = candidate.clip()
		e := deltaEOK(clipped, current.Lab())
		switch {
		case e < gamutJND && gamutJND-e < gamutEpsilon:
			return clipped
		case e < gamutJND:
			loInGamut = false
			lo = current.C
		default:
			hi = current.C
		}
	}
	return clipped
}

// deltaEOK 是两个颜色在 OKLab 里的欧氏距离（规范的 ΔEOK）。
func deltaEOK(c RGB, ref OKLab) float64 {
	o := OKLabFromRGB(c)
	dl, da, db := o.L-ref.L, o.A-ref.A, o.B-ref.B
	return math.Sqrt(dl*dl + da*da + db*db)
}
