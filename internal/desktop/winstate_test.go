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

// mpv 的子窗口每次被改大小都报 window-maximized=假：那是回声，不能把刚最大化的宿主还原回去；
// 宿主还在最大化，就把真写回去（否则 mpv 那边一直是假，CI 冒烟实测到过）。
func TestWindowSyncIgnoresMaximizeEcho(t *testing.T) {
	s := newWindowSync()
	host := hostWindow{Maximized: true}
	s.fromHost(host)
	acts, writes := s.fromMpv("window-maximized", false, host)
	assert.Empty(t, acts, "回声不还原窗口")
	assert.Equal(t, []mpvWrite{{"window-maximized", true}}, writes, "宿主还最大化着：把真写回去")
	acts, writes = s.fromMpv("window-maximized", true, host)
	assert.Empty(t, acts, "写回去的真报回来什么都不做")
	assert.Empty(t, writes, "不来回打乒乓")
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

// CI 冒烟实测到的那一种：还原时宿主写假，紧接着有人请求最大化写真，mpv 把这两下合并成「没变」（上次报的就是真），
// 宿主一个通知都收不到。读回来对一遍才看得见那个请求。
func TestWindowSyncReconcileFindsCoalescedRequest(t *testing.T) {
	s := newWindowSync()
	maxed := hostWindow{Maximized: true}
	s.fromHost(maxed)
	s.fromMpv("window-maximized", true, maxed)
	restored := hostWindow{}
	assert.Equal(t, []mpvWrite{{"window-maximized", false}}, s.fromHost(restored))
	// （mpv 那边：假 → 真，合并之后一个通知都没有）
	mark := s.marks([]string{"window-maximized"})[0]
	acts, writes := s.reconcile("window-maximized", true, restored, mark)
	assert.Equal(t, []winAction{winMaximize}, acts, "读回来是真：有人请求过最大化")
	assert.Empty(t, writes)
	acts, writes = s.fromMpv("window-maximized", true, maxed)
	assert.Empty(t, acts, "迟到的通知不再动")
	assert.Empty(t, writes)
}

// 另一种合并：宿主最大化时写真，子窗口的 WM_SIZE 紧跟着把它改成假，合并之后同样没有通知。读回来是假就再写一次真。
func TestWindowSyncReconcileRewritesClobberedMaximize(t *testing.T) {
	s := newWindowSync()
	s.fromHost(hostWindow{})
	maxed := hostWindow{Maximized: true}
	assert.Equal(t, []mpvWrite{{"window-maximized", true}}, s.fromHost(maxed))
	mark := s.marks([]string{"window-maximized"})[0]
	acts, writes := s.reconcile("window-maximized", false, maxed, mark)
	assert.Empty(t, acts)
	assert.Equal(t, []mpvWrite{{"window-maximized", true}}, writes)
	// 两边一致时读回来什么都不做（全屏 / 置顶 / 最小化同理）
	for _, w := range []mpvWrite{{"window-maximized", true}, {"fullscreen", false}, {"ontop", false}, {"window-minimized", false}} {
		acts, writes = s.reconcile(w.Name, w.Value, maxed, s.marks([]string{w.Name})[0])
		assert.Empty(t, acts, w.Name)
		assert.Empty(t, writes, w.Name)
	}
	acts, writes = s.reconcile("window-maximized", false, hostWindow{Maximized: true, Fullscreen: true}, s.marks([]string{"window-maximized"})[0])
	assert.Empty(t, acts, "全屏时不算最大化")
	assert.Empty(t, writes, "全屏时不算最大化")
}

// 读的这段时间里来过通知、或宿主又写过：读到的可能已经过时，不能照它动窗口（会把用户刚做的撤回去）。
func TestWindowSyncReconcileDropsStaleRead(t *testing.T) {
	s := newWindowSync()
	host := hostWindow{}
	s.fromHost(host)
	mark := s.marks([]string{"fullscreen"})[0]
	// 读回来之前：界面进了全屏又退出（两次通知，宿主照做）
	s.fromMpv("fullscreen", true, host)
	s.fromMpv("fullscreen", false, hostWindow{Fullscreen: true})
	acts, writes := s.reconcile("fullscreen", true, host, mark)
	assert.Empty(t, acts, "过时的「真」不能把窗口又送进全屏")
	assert.Empty(t, writes)
	// 宿主自己写过也一样
	mark = s.marks([]string{"window-maximized"})[0]
	s.fromHost(hostWindow{Maximized: true})
	s.fromHost(host)
	acts, _ = s.reconcile("window-maximized", true, host, mark)
	assert.Empty(t, acts, "读到的是宿主还原之前的真")
}

// 没在最大化时 mpv 报假就是平常的回声，什么都不做；全屏时同理。
func TestWindowSyncMaximizeEchoWhenNotMaximized(t *testing.T) {
	for _, host := range []hostWindow{{}, {Fullscreen: true}, {Maximized: true, Fullscreen: true}} {
		s := newWindowSync()
		s.fromHost(host)
		s.fromMpv("window-maximized", true, host) // 先让记下的值是真，下面那个假才算变化
		acts, writes := s.fromMpv("window-maximized", false, host)
		assert.Empty(t, acts, "%+v", host)
		assert.Empty(t, writes, "%+v", host)
	}
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

// mpv 把宿主写的值报回来之前都算 pending：它还没取过那次改动，之后的请求随时可能与它合并掉，要接着读。
func TestWindowSyncPendingUntilEchoed(t *testing.T) {
	s := newWindowSync()
	names := []string{"fullscreen", "window-maximized", "ontop"}
	host := hostWindow{}
	s.fromHost(host)
	assert.True(t, s.pending(names), "写过去了，还没报回来")
	for _, n := range names {
		s.fromMpv(n, false, host)
	}
	assert.False(t, s.pending(names), "都报回来了")

	maxed := hostWindow{Maximized: true}
	s.fromHost(maxed)
	assert.True(t, s.pending(names))
	s.reconcile("window-maximized", true, maxed, s.marks(names[1:2])[0])
	assert.True(t, s.pending(names), "读回来与写的一样也不算：mpv 没报，就是还没取过")
	s.fromMpv("window-maximized", true, maxed)
	assert.False(t, s.pending(names))

	// 还原时宿主写假，请求的真贴着到、合并掉：读回来照做之后，宿主认为的值与 mpv 上次报的一样，不用再读
	s.fromHost(host)
	assert.True(t, s.pending(names))
	acts, _ := s.reconcile("window-maximized", true, host, s.marks(names[1:2])[0])
	assert.Equal(t, []winAction{winMaximize}, acts)
	assert.False(t, s.pending(names))
}
