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
