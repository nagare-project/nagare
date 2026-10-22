//go:build unix

package mpv

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// launchUI 用内置界面配置起一个空闲的真 mpv，等界面脚本加载好（回了 nagare-event screenshot）再返回。
func launchUI(t *testing.T) *Player {
	t.Helper()
	if testing.Short() {
		t.Skip("-short 模式跳过真 mpv 集成测试")
	}
	mpvPath, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("未安装 mpv，跳过集成测试")
	}
	info, err := Detect(mpvPath)
	require.NoError(t, err)
	dir, err := os.MkdirTemp("", "nagemb")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	configDir, err := PrepareUIConfig(dir)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p, err := Launch(ctx, LaunchOptions{MPVPath: info.Path, SocketDir: dir, ConfigDir: configDir, Title: "nagare 嵌入模式集成测试"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	ready := make(chan struct{})
	go func() {
		for ev := range p.Events() {
			if ev.Kind == EventUIMessage && len(ev.Args) > 0 && ev.Args[0] == "screenshot" {
				close(ready)
				break
			}
		}
		for range p.Events() { // 后面的事件没人要，别让通道堵住
		}
	}()
	require.Eventually(t, func() bool {
		select {
		case <-ready:
			return true
		default:
			_, _ = p.Command("script-binding", "nagare/screenshot")
			return false
		}
	}, 10*time.Second, 200*time.Millisecond, "界面脚本没加载起来，或者 Lua 报错了")
	return p
}

// uiGeometry 是界面画在多大的窗口里、按几倍缩放画（与 main.lua 的 scale() 同一个取法：mpv 自己读得到
// display-hidpi-scale 就用它，读不到用宿主给的）。没有窗口（无显示环境、--vo=null）时界面什么都不画、
// 也不发布 capture，测试跳过而不是失败。
func uiGeometry(t *testing.T, p *Player, hostScale float64) (w, h, s float64) {
	t.Helper()
	var dims struct {
		W float64 `json:"w"`
		H float64 `json:"h"`
	}
	require.NoError(t, p.GetProperty("osd-dimensions", &dims))
	if dims.W <= 0 || dims.H <= 0 {
		t.Skip("没有窗口（无显示环境），界面不画、不发布 capture")
	}
	s = hostScale
	var scale float64
	if err := p.GetProperty("display-hidpi-scale", &scale); err == nil && scale > 0 {
		s = scale
	}
	return dims.W, dims.H, s
}

func boolProperty(p *Player, name string) (value, ok bool) {
	if err := p.GetProperty(name, &value); err != nil {
		return false, false
	}
	return value, true
}

// 验收：命令行 mpv 下（宿主一个属性都没设）界面照旧 —— 自己关边框、不发布 capture / shown、
// 不全屏的 Esc 什么都不做。
func TestIntegration_UICommandLineModeUnchanged(t *testing.T) {
	p := launchUI(t)

	border, ok := boolProperty(p, "border")
	require.True(t, ok)
	assert.False(t, border, "命令行 mpv 下窗口由界面自己画标题栏，边框要关掉")
	_, ok = boolProperty(p, "user-data/nagare/capture")
	assert.False(t, ok, "没嵌入时不发布 capture")
	_, ok = boolProperty(p, "user-data/nagare/shown")
	assert.False(t, ok, "没嵌入时不发布 shown")

	_, err := p.Command("keypress", "ESC")
	require.NoError(t, err)
	select {
	case <-p.Done():
		t.Fatal("命令行 mpv 下不全屏的 Esc 不该退出")
	case <-time.After(800 * time.Millisecond):
	}
}

// 验收：宿主设了 embedded 之后 —— 边框还回去、发布 capture / shown、全屏时 Esc 只退出全屏、
// 不全屏时 Esc 就是返回（mpv 以 quit 正常退出，终态无错误，与用户关窗同一条路）。
func TestIntegration_UIEmbeddedMode(t *testing.T) {
	p := launchUI(t)
	uiGeometry(t, p, 2) // 没有窗口就跳过：界面不画，capture / shown 不会发布，Esc 也不接管

	require.NoError(t, p.SetProperty("user-data/nagare/embedded", true))
	require.NoError(t, p.SetProperty("user-data/nagare/titlebar-inset", 72))
	require.NoError(t, p.SetProperty("user-data/nagare/hidpi", 2))
	require.Eventually(t, func() bool {
		border, ok := boolProperty(p, "border")
		return ok && border
	}, 3*time.Second, 50*time.Millisecond, "嵌入后要把加载时关掉的边框还回去（--wid 下 border=no 会加上改大小的把手）")
	require.Eventually(t, func() bool {
		_, capOK := boolProperty(p, "user-data/nagare/capture")
		_, shownOK := boolProperty(p, "user-data/nagare/shown")
		return capOK && shownOK
	}, 3*time.Second, 50*time.Millisecond, "嵌入后要发布 capture 与 shown")

	require.NoError(t, p.SetProperty("fullscreen", true))
	_, err := p.Command("keypress", "ESC")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		fs, ok := boolProperty(p, "fullscreen")
		return ok && !fs
	}, 3*time.Second, 50*time.Millisecond, "全屏时 Esc 退出全屏")
	select {
	case <-p.Done():
		t.Fatal("全屏时的 Esc 只退出全屏，不能返回")
	case <-time.After(500 * time.Millisecond):
	}

	_, _ = p.Command("keypress", "ESC") // mpv 收到就退出，应答常常赶不上
	requireDoneWithin(t, p, 5*time.Second)
	require.NoError(t, p.Err(), "返回是用户主动结束，终态不能算异常")
}

// 验收：capture 的取值 —— 宿主就靠它决定一次按下是交给界面还是拖动窗口。播放键上是真、空白画面上
// 是假；右键菜单开着时哪里都是真，Esc 关掉菜单之后空白处又是假（而且 mpv 没有因为这下 Esc 退出）。
func TestIntegration_UIEmbeddedCaptureFollowsControls(t *testing.T) {
	p := launchUI(t)
	require.NoError(t, p.SetProperty("user-data/nagare/embedded", true))
	require.NoError(t, p.SetProperty("user-data/nagare/hidpi", 1))
	w, h, s := uiGeometry(t, p, 1)

	// 每次读之前都把模拟光标移过去：真实光标不在窗口上时系统随后会报「鼠标离开」，模拟的位置撑不了多久；
	// capture 在 30ms 的去抖重绘里才更新，等一下再读
	captureAt := func(x, y float64, want bool) func() bool {
		return func() bool {
			if _, err := p.Command("mouse", int(x), int(y)); err != nil {
				return false
			}
			time.Sleep(80 * time.Millisecond)
			got, ok := boolProperty(p, "user-data/nagare/capture")
			return ok && got == want
		}
	}
	playX, playY := w/2, h-44*s // main.lua draw_bottom / draw_transport：大播放键的圆心
	blankX, blankY := w/2, h/2
	require.Eventually(t, captureAt(playX, playY, true), 5*time.Second, 20*time.Millisecond,
		"光标在播放键上时 capture 要是真：这一下按下归界面")
	require.Eventually(t, captureAt(blankX, blankY, false), 5*time.Second, 20*time.Millisecond,
		"光标在空白画面上时 capture 要是假：宿主在这里开始拖动窗口")

	_, err := p.Command("script-binding", "nagare/menu")
	require.NoError(t, err)
	require.Eventually(t, captureAt(blankX, blankY, true), 5*time.Second, 20*time.Millisecond,
		"右键菜单开着时哪里都归界面（点菜单外面是关菜单，不能拖窗）")

	_, err = p.Command("keypress", "ESC")
	require.NoError(t, err)
	require.Eventually(t, captureAt(blankX, blankY, false), 5*time.Second, 20*time.Millisecond,
		"Esc 关掉菜单之后，空白处又归宿主")
	select {
	case <-p.Done():
		t.Fatal("菜单开着时的 Esc 只关菜单，不能接着返回")
	default:
	}
}
