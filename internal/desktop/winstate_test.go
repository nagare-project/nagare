package desktop

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 起播时宿主先把自己的状态写给 mpv：之后 observe_property 报回来的初值与它一致，不会反过来改窗口。
func TestWindowSyncSeedsMpvFromHost(t *testing.T) {
	s := newWindowSync()
	host := hostWindow{Fullscreen: true}
	assert.Equal(t, []mpvWrite{{"fullscreen", true}, {"window-maximized", false}, {"ontop", false}}, s.fromHost(host))
	for _, name := range []string{"fullscreen", "window-maximized", "ontop"} {
		acts, writes := s.fromMpv(name, name == "fullscreen", host)
		assert.Empty(t, acts, name)
		assert.Empty(t, writes, name)
	}
	assert.Empty(t, s.fromHost(host), "没变就不再写")
}

// Lua 的 cycle fullscreen（Enter、双击）落到宿主窗口；退出同理。宿主照做之后再报一次也不会重复。
func TestWindowSyncFullscreenFromMpv(t *testing.T) {
	s := newWindowSync()
	host := hostWindow{}
	s.fromHost(host)
	acts, _ := s.fromMpv("fullscreen", true, host)
	assert.Equal(t, []winAction{winEnterFullscreen}, acts)
	host.Fullscreen = true
	assert.Empty(t, s.fromHost(host), "mpv 已经是这个值，不用写回")
	acts, _ = s.fromMpv("fullscreen", false, host)
	assert.Equal(t, []winAction{winExitFullscreen}, acts)
}

// 用户在宿主窗口上的操作（标题栏最大化、Win+↑、置顶）写回 mpv，界面的图标才对得上。
func TestWindowSyncHostChangesWriteBack(t *testing.T) {
	s := newWindowSync()
	s.fromHost(hostWindow{})
	assert.Equal(t, []mpvWrite{{"window-maximized", true}}, s.fromHost(hostWindow{Maximized: true}))
	assert.Equal(t, []mpvWrite{{"fullscreen", true}, {"window-maximized", false}},
		s.fromHost(hostWindow{Maximized: true, Fullscreen: true}), "全屏时不算最大化")
}

// mpv 的子窗口每次被改大小都报 window-maximized=假：那是回声，不能把刚最大化的宿主还原回去。
func TestWindowSyncIgnoresMaximizeEcho(t *testing.T) {
	s := newWindowSync()
	host := hostWindow{Maximized: true}
	s.fromHost(host)
	acts, writes := s.fromMpv("window-maximized", false, host)
	assert.Empty(t, acts)
	assert.Empty(t, writes)
	// 有人真的请求最大化（cycle / set）才动
	host.Maximized = false
	s.fromHost(host)
	acts, _ = s.fromMpv("window-maximized", true, host)
	assert.Equal(t, []winAction{winMaximize}, acts)
	// 全屏时不最大化
	s2 := newWindowSync()
	fs := hostWindow{Fullscreen: true}
	s2.fromHost(fs)
	acts, _ = s2.fromMpv("window-maximized", true, fs)
	assert.Empty(t, acts)
}

// 最小化是一次性的请求：宿主最小化，mpv 的值放回假，下次还能再请求。
func TestWindowSyncMinimizeIsOneShot(t *testing.T) {
	s := newWindowSync()
	host := hostWindow{}
	s.fromHost(host)
	acts, writes := s.fromMpv("window-minimized", true, host)
	assert.Equal(t, []winAction{winMinimize}, acts)
	assert.Equal(t, []mpvWrite{{"window-minimized", false}}, writes)
	acts, writes = s.fromMpv("window-minimized", false, host)
	assert.Empty(t, acts, "写回去的假报回来不算请求")
	assert.Empty(t, writes)
	acts, _ = s.fromMpv("window-minimized", true, host)
	assert.Equal(t, []winAction{winMinimize}, acts, "再请求一次照样最小化")
}

// t 键的 cycle ontop 落到宿主窗口的置顶上。
func TestWindowSyncOntop(t *testing.T) {
	s := newWindowSync()
	host := hostWindow{}
	s.fromHost(host)
	acts, _ := s.fromMpv("ontop", true, host)
	assert.Equal(t, []winAction{winTopmostOn}, acts)
	host.Topmost = true
	acts, _ = s.fromMpv("ontop", false, host)
	assert.Equal(t, []winAction{winTopmostOff}, acts)
}

// 不认识的属性什么都不做。
func TestWindowSyncUnknownProperty(t *testing.T) {
	s := newWindowSync()
	acts, writes := s.fromMpv("pause", true, hostWindow{})
	assert.Empty(t, acts)
	assert.Empty(t, writes)
}
