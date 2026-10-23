//go:build windows

package desktop

import (
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var pSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")

// sendFromOtherThread 像 mpv 的窗口线程那样，从另一个线程把一条消息「发」（SendMessage）给顶层窗口，返回花了多久。
func sendFromOtherThread(t *testing.T, hwnd uintptr, m uint32, w, l uintptr) time.Duration {
	t.Helper()
	took := make(chan time.Duration, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		start := time.Now()
		var res uintptr
		// SMTO_ABORTIFHUNG：真卡住的话测试别跟着挂死
		pSendMessageTimeoutW.Call(hwnd, uintptr(m), w, l, 0x0002, 3000, uintptr(unsafe.Pointer(&res)))
		took <- time.Since(start)
	}()
	select {
	case d := <-took:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("跨线程的 SendMessage 没回来")
		return 0
	}
}

// 播放时 mpv 的窗口线程把单按 Alt（SC_KEYMENU）跨线程发过来：必须马上回去，不能让 mpv 的线程等我们的菜单循环
// （CI 实测过：等的话 mpv 的窗口 8 秒后被看门狗当卡死结束）。之后界面线程照常响应、没有进菜单模式。
func TestCrossThreadKeyMenuNeverBlocksSender(t *testing.T) {
	l := startLoop(t, true)
	a := l.a
	var top uintptr
	l.onUI(t, func() {
		a.beginSession()
		top = a.top
	})
	assert.Less(t, sendFromOtherThread(t, top, wmSysCommand, scKeyMenu, 0), time.Second)
	l.onUI(t, func() {})
}

// mpv 每试一种 GPU 上下文就重建一次窗口（每个都是 disabled 的）：看门狗发现接上的窗口没了 / 又被禁用，就换成新的
// 那个并重新启用。这里用本进程建的「mpv」类窗口模拟 mpv 的子窗口。
func TestStaleChildIsRescannedAndReenabled(t *testing.T) {
	if ipcInput {
		t.Skip("IPC 转发模式下不重新启用子窗口")
	}
	l := startLoop(t, true)
	a := l.a
	var host uintptr
	var regErr error
	l.onUI(t, func() {
		regErr = registerClass("mpv", hostProcCb, 0, 0)
		host = a.videoHost
	})
	require.NoError(t, regErr)
	fake := func() uintptr {
		var h uintptr
		var err error
		l.onUI(t, func() {
			h, err = createWindow(0, "mpv", "", wsChild|wsVisible, 0, 0, 100, 100, host)
			if err == nil {
				pEnableWindow.Call(h, 0) // mpv 在 --wid 下把自己的窗口建成 disabled
			}
		})
		require.NoError(t, err)
		return h
	}
	s := newVideoSession(999, nil, host) // 不是界面上的当前会话：attachChild 什么都不做
	s.pid.Store(uint32(os.Getpid()))

	enabledSoon := func(h uintptr, msg string) {
		t.Helper()
		assert.Eventually(t, func() bool { return boolCall(pIsWindowEnabled, h) && !s.enabling.Load() }, 3*time.Second, 10*time.Millisecond, msg)
	}

	first := fake()
	require.True(t, s.adoptChild())
	assert.Equal(t, first, s.child.Load())
	enabledSoon(first, "接上时重新启用")
	assert.False(t, s.childStale())

	// mpv 退到下一个 GPU 上下文：旧窗口销毁，新窗口又是 disabled 的
	l.onUI(t, func() { pDestroyWindow.Call(first) })
	second := fake()
	assert.True(t, s.childStale(), "接上的窗口没了")
	require.True(t, s.adoptChild())
	assert.Equal(t, second, s.child.Load())
	enabledSoon(second, "新窗口也重新启用")

	// 同一个窗口又被禁用了：也算过期，重新启用
	l.onUI(t, func() { pEnableWindow.Call(second, 0) })
	assert.True(t, s.childStale())
	require.True(t, s.adoptChild())
	enabledSoon(second, "又被禁用的窗口重新启用")
}

// mpv 的窗口线程卡住时 EnableWindow（跨进程的 SendMessage）回不来：接窗口的一方不能跟着等 —— 看门狗要照常走到
// IsHungAppWindow 那一步。这里用一个不处理消息的线程建「mpv」窗口模拟卡住的 mpv。
func TestAdoptChildDoesNotWaitForAHungWindow(t *testing.T) {
	if ipcInput {
		t.Skip("IPC 转发模式下不重新启用子窗口")
	}
	l := startLoop(t, true)
	var host uintptr
	var regErr error
	l.onUI(t, func() {
		regErr = registerClass("mpv", hostProcCb, 0, 0)
		host = l.a.videoHost
	})
	require.NoError(t, regErr)

	created := make(chan uintptr, 1)
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		h, err := createWindow(0, "mpv", "", wsChild|wsVisible, 0, 0, 100, 100, host)
		if err != nil {
			created <- 0
			return
		}
		pEnableWindow.Call(h, 0) // 自己线程上的窗口：立刻生效
		created <- h
		<-release // 这期间不处理消息：别的线程对这个窗口 SendMessage 就一直等着，像卡住的 mpv
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) && !boolCall(pIsWindowEnabled, h) {
			drainMessages()
			time.Sleep(10 * time.Millisecond)
		}
		pDestroyWindow.Call(h)
		drainMessages()
	}()
	h := <-created
	require.NotZero(t, h)

	s := newVideoSession(998, nil, host)
	s.pid.Store(uint32(os.Getpid()))
	start := time.Now()
	require.True(t, s.adoptChild())
	assert.Less(t, time.Since(start), 500*time.Millisecond, "接窗口不等 EnableWindow 回来")
	assert.True(t, s.enabling.Load(), "启用在后台等着")
	assert.True(t, s.childStale(), "还没启用上：看门狗下一圈仍会看它是不是卡住了")
	require.True(t, s.adoptChild(), "再接一次也不再起第二个等待")

	close(release) // 「mpv」恢复处理消息：启用落地
	assert.Eventually(t, func() bool { return boolCall(pIsWindowEnabled, h) && !s.enabling.Load() }, 3*time.Second, 10*time.Millisecond)
	<-done
}
