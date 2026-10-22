//go:build unix

package mpv

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 验收：控制条后面的渐变遮罩没有横纹。遮罩是一条条半透明矩形拼出来的，拼缝只要有重叠或者落在
// 半个像素上，那一行的透明度就叠两次，纯色画面上看得出一道道深色横线（以前每条多盖 0.6 像素防缝，
// 顶部 20 道、底部 24 道全是）。在纯灰画面上截下整个窗口，顶部遮罩从上往下必须一路变亮，不能有
// 往回变暗的行。
func TestIntegration_UIScrimHasNoStripes(t *testing.T) {
	p := launchUI(t)
	require.NoError(t, p.SetProperty("screenshot-high-bit-depth", false))
	_, err := p.Command("loadfile", "av://lavfi:color=c=0x808080:s=1280x720:r=24")
	require.NoError(t, err)
	require.NoError(t, p.SetProperty("pause", true))
	require.Eventually(t, func() bool {
		var w float64
		return p.GetProperty("width", &w) == nil && w > 0
	}, 10*time.Second, 50*time.Millisecond, "纯灰画面没有加载出来")
	waitStableOSD(t, p)
	w, h, s := uiGeometry(t, p, 1)
	if _, err := p.Command("mouse", int(w/2), int(h/2)); err != nil { // 暂停时控制条本来就露着，移一下只是保险
		t.Logf("移动模拟光标失败：%v", err)
	}
	time.Sleep(400 * time.Millisecond)

	// 稳态才是用户看到的：窗口刚按视频大小调整过、界面还没按新尺寸重画（30ms 去抖，机器忙时更久）的那一帧，
	// 画布被拉伸、条的边界落回半个像素，相邻两条之间会透出一道浅线。看到拼缝就隔 0.5 秒再截，四次都有才算失败。
	var row int
	var step float64
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		lum := topScrimLuma(t, p, s, h)
		require.Less(t, lum[0]+20, lum[len(lum)-1], "顶部遮罩应该从上往下明显变亮（截图里没有遮罩？）")
		if row, step = firstDarkerStep(lum); row < 0 {
			return
		}
	}
	t.Fatalf("第 %d 行比上一行暗 %.1f：遮罩的拼缝出现横纹（隔 0.5 秒连截四次都有）", row, step)
}

// topScrimLuma 截下整个窗口，返回顶部遮罩里每一行（中间那一段：标题在左、窗口按钮在右）的平均亮度。
func topScrimLuma(t *testing.T, p *Player, s, osdH float64) []float64 {
	t.Helper()
	shot := filepath.Join(t.TempDir(), fmt.Sprintf("scrim-%d.png", time.Now().UnixNano()))
	if _, err := p.Command("screenshot-to-file", shot, "window"); err != nil {
		t.Skipf("这台机器上截不了窗口（%v）", err)
	}
	f, err := os.Open(shot)
	require.NoError(t, err)
	defer f.Close()
	img, err := png.Decode(f)
	require.NoError(t, err)

	b := img.Bounds()
	// 截图与 OSD 尺寸不一定相同（缩放）：按比例换算顶部遮罩的高度（main.lua draw_top：140 × 缩放）
	rows := int(140 * s * float64(b.Dy()) / osdH)
	x0, x1 := b.Min.X+b.Dx()*42/100, b.Min.X+b.Dx()*58/100
	lum := make([]float64, rows)
	for y := 0; y < rows; y++ {
		var sum float64
		for x := x0; x < x1; x++ {
			r, g, bl, _ := img.At(x, b.Min.Y+y).RGBA()
			sum += float64(r+g+bl) / 3 / 257
		}
		lum[y] = sum / float64(x1-x0)
	}
	return lum
}

// firstDarkerStep 找第一行比上一行暗 0.5 级以上的地方（0.5 以内是量化噪声；拼缝重叠的横纹是 10 级以上）。
// 没有返回 -1。
func firstDarkerStep(lum []float64) (row int, step float64) {
	for y := 1; y < len(lum); y++ {
		if d := lum[y-1] - lum[y]; d > 0.5 {
			return y, d
		}
	}
	return -1, 0
}

// waitStableOSD 等窗口尺寸稳定下来（连续 0.5 秒不变）：加载画面后窗口会按视频大小重新调整，
// 界面按新的 osd-dimensions 重画之前截图，画布分辨率与窗口像素对不上，整数坐标又落回半个像素，
// 量出来的横纹是这一瞬的、不是遮罩画法的（用户稳定观看时看到的才是要验的）。
func waitStableOSD(t *testing.T, p *Player) {
	t.Helper()
	type dims struct {
		W float64 `json:"w"`
		H float64 `json:"h"`
	}
	var last dims
	stable := 0
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var d dims
		if err := p.GetProperty("osd-dimensions", &d); err == nil && d == last && d.W > 0 {
			stable++
			if stable >= 5 {
				time.Sleep(150 * time.Millisecond) // 界面 30ms 去抖重画，留足余量
				return
			}
		} else {
			stable, last = 0, d
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Log("窗口尺寸 8 秒内没有稳定下来，照常往下测")
}
