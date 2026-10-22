//go:build windows

package desktop

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// testLoop 在一条锁住的线程上跑真的消息循环（与 nagare 的主线程同一套：消息窗口、uiQueue、pumpUntilQuit）。
type testLoop struct {
	a        *app
	tid      uint32
	done     chan struct{}
	stopOnce sync.Once
}

// startLoop 起一个消息循环；withWindows 时连顶层窗口与两个子窗口一起建（不建 WebView2）。
func startLoop(t *testing.T, withWindows bool) *testLoop {
	t.Helper()
	old := ui
	a := &app{}
	ui = a
	l := &testLoop{a: a, done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread() // 不解锁：goroutine 结束时这条线程随之结束，窗口一起销毁
		defer close(l.done)
		l.tid = currentThreadID()
		var err error
		if withWindows {
			err = a.createWindows()
			a.web = &webLayer{} // 占位：测试里不建 WebView2（ready 为假，网页层的操作都是空操作）
		} else if err = registerClass(classDispatch, dispatchProcCb, 0, 0); err == nil {
			a.dispatch, err = createWindow(0, classDispatch, "", 0, 0, 0, 0, 0, hwndMessage)
		}
		if err != nil {
			ready <- err
			return
		}
		a.startQueue()
		ready <- nil
		pumpUntilQuit()
		a.destroyWindows()
		a.q.close()
	}()
	require.NoError(t, <-ready)
	t.Cleanup(func() {
		l.stop(t)
		ui = old
	})
	return l
}

func (l *testLoop) stop(t *testing.T) {
	l.stopOnce.Do(func() {
		l.a.q.post(func() { pPostQuitMessage.Call(0) })
		select {
		case <-l.done:
		case <-time.After(5 * time.Second):
			t.Error("消息循环没停下来")
		}
	})
}

// onUI 在界面线程上做 f 并等它做完。
func (l *testLoop) onUI(t *testing.T, f func()) {
	t.Helper()
	require.NoError(t, l.a.q.call(5*time.Second, f))
}

// 任意 goroutine 排的活都在界面线程上做，一件不丢。
func TestLoopRunsPostedWorkOnUIThread(t *testing.T) {
	l := startLoop(t, false)
	var n atomic.Int32
	var wrong atomic.Int32
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				l.a.q.post(func() {
					if currentThreadID() != l.tid {
						wrong.Add(1)
					}
					n.Add(1)
				})
			}
		}()
	}
	wg.Wait()
	l.onUI(t, func() {})
	assert.Equal(t, int32(400), n.Load())
	assert.Zero(t, wrong.Load(), "有活跑在了别的线程上")
}

// 在界面线程上再 call 不能等自己（直接做）。
func TestLoopCallFromUIThreadRunsInline(t *testing.T) {
	l := startLoop(t, false)
	inner := false
	var err error
	l.onUI(t, func() { err = l.a.q.call(time.Second, func() { inner = true }) })
	require.NoError(t, err)
	assert.True(t, inner)
}

// pumpWait 等的时候照常处理消息：别的 goroutine 排进来的活照样做，做完了等的事件才有信号。
func TestPumpWaitServicesMessagesWhileWaiting(t *testing.T) {
	l := startLoop(t, false)
	var ok, ran bool
	l.onUI(t, func() {
		ev, err := windows.CreateEvent(nil, 1, 0, nil)
		if !assert.NoError(t, err) {
			return
		}
		defer windows.CloseHandle(ev)
		go l.a.q.post(func() {
			ran = true
			windows.SetEvent(ev)
		})
		ok = l.a.pumpWait(ev, 5*time.Second)
	})
	assert.True(t, ok)
	assert.True(t, ran)
}

// 等不到就按时返回。
func TestPumpWaitTimesOut(t *testing.T) {
	l := startLoop(t, false)
	var ok bool
	var took time.Duration
	l.onUI(t, func() {
		ev, err := windows.CreateEvent(nil, 1, 0, nil)
		if !assert.NoError(t, err) {
			return
		}
		defer windows.CloseHandle(ev)
		start := time.Now()
		ok = l.a.pumpWait(ev, 150*time.Millisecond)
		took = time.Since(start)
	})
	assert.False(t, ok)
	assert.Less(t, took, 2*time.Second)
}

// 嵌套等待里取到的 WM_QUIT 要补投：外层的消息循环照样停得下来（注销时的 WM_ENDSESSION 就在窗口过程里等）。
func TestPumpWaitRepostsQuit(t *testing.T) {
	l := startLoop(t, false)
	l.a.q.post(func() {
		pPostQuitMessage.Call(0)
		l.a.q.post(func() {}) // 一条普通消息把等待叫醒，接着 PeekMessage 就会取到 WM_QUIT
		ev, _ := windows.CreateEvent(nil, 1, 0, nil)
		defer windows.CloseHandle(ev)
		l.a.pumpWait(ev, 200*time.Millisecond)
	})
	select {
	case <-l.done:
	case <-time.After(5 * time.Second):
		t.Fatal("WM_QUIT 被嵌套等待吞掉了，外层循环没停")
	}
}

// pumpUntilDone：收尾完成（done 关闭）就返回，期间照常处理消息。
func TestPumpUntilDone(t *testing.T) {
	l := startLoop(t, false)
	done := make(chan struct{})
	ran := false
	l.onUI(t, func() {
		l.a.done = done
		go func() {
			l.a.q.post(func() { ran = true })
			time.Sleep(100 * time.Millisecond)
			close(done)
		}()
		start := time.Now()
		l.a.pumpUntilDone(5 * time.Second)
		assert.Less(t, time.Since(start), 3*time.Second)
	})
	assert.True(t, ran)
}
