//go:build windows

package desktop

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 真 Windows 上的窗口（不建 WebView2、不起 mpv）：两层切换、全屏进出、mpv 的请求落到窗口上、关窗口只是藏起来。

// 起播换到画面层，会话结束换回网页层；过期会话（被新的顶掉的）结束时什么都不动。
func TestWindowSwapsLayersPerSession(t *testing.T) {
	l := startLoop(t, true)
	a := l.a
	var first, second sessionPrep
	l.onUI(t, func() { first = a.beginSession() })
	require.True(t, first.ok)
	assert.Equal(t, a.videoHost, first.host)
	l.onUI(t, func() {
		assert.Equal(t, modeVideo, a.mode)
		assert.True(t, boolCall(pIsWindowVisible, a.videoHost), "画面层要露出来")
		assert.False(t, boolCall(pIsWindowVisible, a.webHost), "网页层要藏起来")
		assert.True(t, boolCall(pIsWindowVisible, a.top), "起播把窗口显示出来")
		second = a.beginSession() // 换片：新会话顶掉旧的
		a.endSession(first.id)    // 旧 mpv 迟到的退出
		assert.Equal(t, modeVideo, a.mode, "过期会话结束不能把新会话换回网页")
		a.endSession(second.id)
		assert.Equal(t, modeWeb, a.mode)
		assert.True(t, boolCall(pIsWindowVisible, a.webHost))
		assert.False(t, boolCall(pIsWindowVisible, a.videoHost))
	})
}

// 无边框全屏：铺满显示器、去掉标题栏；退出时样式与位置原样还回去。网页层时不响应 mpv 的全屏请求。
func TestWindowFullscreenRoundTrip(t *testing.T) {
	l := startLoop(t, true)
	a := l.a
	l.onUI(t, func() {
		a.beginSession()
		before := windowRect(a.top)
		style, _, _ := pGetWindowLongPtrW.Call(a.top, gwlStyle)
		a.apply([]winAction{winEnterFullscreen})
		assert.True(t, a.hostState().Fullscreen)
		fsStyle, _, _ := pGetWindowLongPtrW.Call(a.top, gwlStyle)
		assert.NotZero(t, fsStyle&wsPopup, "全屏时是弹出式窗口")
		assert.Zero(t, fsStyle&0x00C00000, "全屏时没有标题栏（WS_CAPTION）")
		a.apply([]winAction{winExitFullscreen})
		assert.False(t, a.hostState().Fullscreen)
		after, _, _ := pGetWindowLongPtrW.Call(a.top, gwlStyle)
		assert.Equal(t, style, after)
		assert.Equal(t, before, windowRect(a.top))

		a.endSession(a.sessionID)
		a.apply([]winAction{winEnterFullscreen})
		assert.False(t, a.hostState().Fullscreen, "网页层时 mpv 的全屏请求不算数")
	})
}

// mpv 的最大化、置顶请求落到窗口上；换回网页时置顶解除（只管播放的时候）。
func TestWindowAppliesMpvRequests(t *testing.T) {
	l := startLoop(t, true)
	a := l.a
	l.onUI(t, func() {
		prep := a.beginSession()
		a.apply([]winAction{winMaximize})
		assert.True(t, a.hostState().Maximized)
		a.apply([]winAction{winTopmostOn})
		ex, _, _ := pGetWindowLongPtrW.Call(a.top, ^uintptr(19)) // GWL_EXSTYLE = -20
		assert.NotZero(t, ex&0x8, "WS_EX_TOPMOST")
		a.endSession(prep.id)
		ex, _, _ = pGetWindowLongPtrW.Call(a.top, ^uintptr(19))
		assert.Zero(t, ex&0x8, "回到网页解除置顶")
	})
}

// 关窗口（WM_CLOSE：标题栏的 ×、Alt+F4）只是藏起来，窗口还在；nagare 照旧在后台。
func TestWindowCloseHidesInsteadOfDestroying(t *testing.T) {
	l := startLoop(t, true)
	a := l.a
	dir := t.TempDir()
	a.opts.DataDir = dir
	l.onUI(t, func() { a.bringToFront() })
	l.onUI(t, func() { topProc(a.top, wmClose, 0, 0) })
	l.onUI(t, func() {
		assert.True(t, boolCall(pIsWindow, a.top), "窗口不能被销毁")
		assert.False(t, boolCall(pIsWindowVisible, a.top))
	})
	_, err := os.Stat(dir + `\` + placementFile)
	assert.NoError(t, err, "关窗口时记下位置")
}

// 记下的位置下次放回去；位置不在任何显示器上就不用。
func TestWindowPlacementRoundTrip(t *testing.T) {
	l := startLoop(t, true)
	a := l.a
	a.opts.DataDir = t.TempDir()
	var saved rect
	l.onUI(t, func() {
		a.bringToFront()
		pSetWindowPos.Call(a.top, 0, 100, 120, 900, 600, swpNoZOrder|swpNoActivate)
		a.savePlacement()
		saved = windowRect(a.top)
		p, ok := a.loadPlacement()
		assert.True(t, ok)
		assert.Equal(t, saved.width(), p.Right-p.Left)
	})
	require.NoError(t, os.WriteFile(a.placementPath(), []byte(`{"Left":-90000,"Top":-90000,"Right":-89000,"Bottom":-89400}`), 0o600))
	l.onUI(t, func() {
		_, ok := a.loadPlacement()
		assert.False(t, ok, "不在任何显示器上的位置不用")
	})
}
