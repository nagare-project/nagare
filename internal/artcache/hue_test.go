package artcache

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/colorspace"
)

// 测试图全部在代码里现画：纯色块、条纹、渐变，不用任何真实海报。

// sRGB 三原色的 OKLCH 色相（公开参照值）。
const (
	hueRed   = 29.23
	hueGreen = 142.50
	hueBlue  = 264.05
)

var (
	red   = color.NRGBA{255, 0, 0, 255}
	green = color.NRGBA{0, 255, 0, 255}
	blue  = color.NRGBA{0, 0, 255, 255}
)

// paint 画一张 w×h 的图：fill(x, y) 决定每个像素的颜色。
func paint(w, h int, fill func(x, y int) color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, fill(x, y))
		}
	}
	return img
}

func solid(c color.NRGBA) func(int, int) color.NRGBA {
	return func(int, int) color.NRGBA { return c }
}

func grey(v uint8) color.NRGBA { return color.NRGBA{v, v, v, 255} }

// oklchPixel 把一个 OKLCH 颜色取成 8 位像素（测试用，颜色本身在色域内）。
func oklchPixel(l, c, h float64) color.NRGBA {
	r, g, b := colorspace.ToSRGB(colorspace.OKLCH{L: l, C: c, H: h}).Bytes()
	return color.NRGBA{r, g, b, 255}
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func sample(t *testing.T, data []byte) (float64, toneReason) {
	t.Helper()
	hue, reason, err := sampleHue(bytes.NewReader(data))
	require.NoError(t, err)
	return hue, reason
}

func hueDiff(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	return math.Min(d, 360-d)
}

func TestSampleHueSolidColours(t *testing.T) {
	cases := []struct {
		name string
		img  image.Image
		want float64
	}{
		{"红", paint(40, 60, solid(red)), hueRed},
		{"绿", paint(40, 60, solid(green)), hueGreen},
		{"蓝", paint(40, 60, solid(blue)), hueBlue},
		{"比采样网格大的图", paint(500, 700, solid(blue)), hueBlue},
		{"1×1", paint(1, 1, solid(green)), hueGreen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hue, reason := sample(t, encodePNG(t, tc.img))
			require.Empty(t, reason)
			assert.InDelta(t, 0, hueDiff(tc.want, hue), 0.1, "得到 %.2f", hue)
		})
	}
}

func TestSampleHueJPEGAndGIF(t *testing.T) {
	var jp bytes.Buffer
	require.NoError(t, jpeg.Encode(&jp, paint(64, 96, solid(red)), &jpeg.Options{Quality: 90}))
	hue, reason := sample(t, jp.Bytes())
	require.Empty(t, reason)
	assert.InDelta(t, 0, hueDiff(hueRed, hue), 2, "JPEG 的色度会有一点损失，得到 %.2f", hue)

	var gf bytes.Buffer
	require.NoError(t, gif.Encode(&gf, paint(64, 96, solid(blue)), nil))
	hue, reason = sample(t, gf.Bytes())
	require.Empty(t, reason)
	assert.InDelta(t, 0, hueDiff(hueBlue, hue), 1, "得到 %.2f", hue)
}

// 面积大的那一簇赢：七成蓝、三成红 → 蓝；不会把两种颜色平均成紫。
func TestSampleHueLargestClusterWins(t *testing.T) {
	img := paint(100, 100, func(x, _ int) color.NRGBA {
		if x < 70 {
			return blue
		}
		return red
	})
	hue, reason := sample(t, encodePNG(t, img))
	require.Empty(t, reason)
	assert.InDelta(t, 0, hueDiff(hueBlue, hue), 0.5, "得到 %.2f", hue)
}

// 灰阶不投票：大片灰渐变上的一成绿色就是这张封面的颜色。
func TestSampleHueIgnoresGreyArea(t *testing.T) {
	img := paint(100, 100, func(x, y int) color.NRGBA {
		if y < 10 {
			return green
		}
		return grey(uint8(x * 255 / 99))
	})
	hue, reason := sample(t, encodePNG(t, img))
	require.Empty(t, reason)
	assert.InDelta(t, 0, hueDiff(hueGreen, hue), 0.5, "得到 %.2f", hue)
}

// 一簇颜色压在两个 10° 格子的边界上，不会被劈开：结果在两者中间。
func TestSampleHueAcrossBinBoundary(t *testing.T) {
	a, b := oklchPixel(0.7, 0.12, 8), oklchPixel(0.7, 0.12, 12)
	img := paint(100, 100, func(x, _ int) color.NRGBA {
		if x%2 == 0 {
			return a
		}
		return b
	})
	hue, reason := sample(t, encodePNG(t, img))
	require.Empty(t, reason)
	assert.InDelta(t, 0, hueDiff(10, hue), 1, "得到 %.2f", hue)
}

func TestSampleHueGrayscale(t *testing.T) {
	cases := map[string]image.Image{
		"灰渐变":   paint(100, 100, func(x, _ int) color.NRGBA { return grey(uint8(x * 255 / 99)) }),
		"黑白棋盘":  paint(64, 64, func(x, y int) color.NRGBA { return grey(uint8(255 * ((x/8 + y/8) % 2))) }),
		"微弱色偏":  paint(64, 64, solid(color.NRGBA{128, 129, 128, 255})),
		"一点点红字": paint(100, 100, func(x, y int) color.NRGBA { return map[bool]color.NRGBA{true: red, false: grey(200)}[x < 10 && y < 10] }),
		"全透明":   paint(32, 32, solid(color.NRGBA{255, 0, 0, 0})),
	}
	for name, img := range cases {
		t.Run(name, func(t *testing.T) {
			_, reason := sample(t, encodePNG(t, img))
			assert.Equal(t, reasonGrayscale, reason)
		})
	}
}

// 透明像素底下的颜色不算：只看看得见的部分。
func TestSampleHueSkipsTransparentPixels(t *testing.T) {
	img := paint(100, 100, func(x, _ int) color.NRGBA {
		if x < 30 {
			return green
		}
		return color.NRGBA{255, 0, 0, 0} // 透明的「红」
	})
	hue, reason := sample(t, encodePNG(t, img))
	require.Empty(t, reason)
	assert.InDelta(t, 0, hueDiff(hueGreen, hue), 0.5, "得到 %.2f", hue)
}

// 子图的 Bounds().Min 不是原点：采样坐标要从 Min 起算，只看子图里的像素。
func TestDominantHueRespectsBoundsOrigin(t *testing.T) {
	full := paint(200, 200, func(x, y int) color.NRGBA {
		if x >= 100 && y >= 100 {
			return green
		}
		return red
	})
	hue, ok := dominantHue(full.SubImage(image.Rect(100, 100, 200, 200)))
	require.True(t, ok)
	assert.InDelta(t, 0, hueDiff(hueGreen, hue), 0.5, "得到 %.2f", hue)
}

// 半透明：alpha 过半的像素按反预乘后的颜色采样，不到一半的跳过。
func TestSampleHueSemiTransparent(t *testing.T) {
	img := paint(100, 100, func(x, _ int) color.NRGBA {
		if x < 40 {
			return color.NRGBA{0, 0, 255, 0xa0} // 六成多不透明的蓝：参与，色相不因预乘变化
		}
		return color.NRGBA{255, 0, 0, 0x60} // 不到一半的红：跳过
	})
	hue, reason := sample(t, encodePNG(t, img))
	require.Empty(t, reason)
	assert.InDelta(t, 0, hueDiff(hueBlue, hue), 0.5, "得到 %.2f", hue)
}

// 读的过程中出 I/O 错误（*fs.PathError）是「以后再试」，不是「这张图解不了」。
func TestSampleHueIOErrorIsNotADecodeFailure(t *testing.T) {
	pathErr := &fs.PathError{Op: "read", Path: "cover.img", Err: errors.New("模拟的磁盘错误")}
	// 嗅探格式时就读失败：标准库会把它说成「不认识的格式」，这里必须认出是 I/O 错误
	_, reason, err := sampleHue(failingReader{err: pathErr})
	assert.Empty(t, reason)
	assert.ErrorIs(t, err, pathErr.Err)

	// 文件头读完、像素数据读到一半出错
	full := encodePNG(t, paint(64, 64, func(x, y int) color.NRGBA { return color.NRGBA{uint8(x * 4), uint8(y * 4), 90, 255} }))
	_, reason, err = sampleHue(&brokenAfter{data: full, limit: len(full) / 2, err: pathErr})
	assert.Empty(t, reason)
	assert.ErrorIs(t, err, pathErr.Err)
}

// brokenAfter 先正常给出前 limit 个字节，之后每次读都报 err。
type brokenAfter struct {
	data  []byte
	limit int
	pos   int64
	err   error
}

func (b *brokenAfter) Read(p []byte) (int, error) {
	if b.pos >= int64(b.limit) {
		return 0, b.err
	}
	n := copy(p, b.data[b.pos:b.limit])
	b.pos += int64(n)
	return n, nil
}

func (b *brokenAfter) Seek(off int64, whence int) (int64, error) {
	if whence == io.SeekStart {
		b.pos = off
	}
	return b.pos, nil
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error)       { return 0, r.err }
func (r failingReader) Seek(int64, int) (int64, error) { return 0, nil }

func TestSampleHueRejectsUnsupportedAndBroken(t *testing.T) {
	// WebP 的文件头（RIFF....WEBPVP8 ）：标准库没有解码器 → 不支持，不是 I/O 错误
	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00"), make([]byte, 24)...)
	_, reason := sample(t, webp)
	assert.Equal(t, reasonUnsupported, reason)

	_, reason = sample(t, []byte("<html>not an image</html>"))
	assert.Equal(t, reasonUnsupported, reason)

	// 截断的 PNG：文件头完好、像素数据缺一半
	full := encodePNG(t, paint(64, 64, func(x, y int) color.NRGBA { return color.NRGBA{uint8(x * 4), uint8(y * 4), 90, 255} }))
	_, reason = sample(t, full[:len(full)/2])
	assert.Equal(t, reasonDecode, reason)
}

// 像素上限只看文件头：声称 4000 万像素以上的图不解码（这里的「图」只有一个 IHDR）。
func TestSampleHuePixelCap(t *testing.T) {
	_, reason := sample(t, pngHeaderOnly(8001, 5000, 8))
	assert.Equal(t, reasonTooLarge, reason, "4000.5 万像素")

	// 恰好 4000 万像素放行，进了解码才因为没有像素数据失败
	_, reason = sample(t, pngHeaderOnly(8000, 5000, 8))
	assert.Equal(t, reasonDecode, reason)

	// 16 位色深每像素 8 字节：3000 万像素就要 240MB，按解码后的字节数拒绝
	_, reason = sample(t, pngHeaderOnly(6000, 5000, 16))
	assert.Equal(t, reasonTooLarge, reason)
	_, reason = sample(t, pngHeaderOnly(5000, 4000, 16))
	assert.Equal(t, reasonDecode, reason, "2000 万像素的 16 位图放行")
}

// pngHeaderOnly 是只有签名 + IHDR 的 PNG：DecodeConfig 读得出尺寸，Decode 必然失败。
func pngHeaderOnly(w, h uint32, depth byte) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = depth, 6 // RGBA
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	buf.Write(chunk)
	_ = binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return buf.Bytes()
}
