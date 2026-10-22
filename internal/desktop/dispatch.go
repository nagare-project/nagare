package desktop

import (
	"errors"
	"sync"
	"time"
)

// uiQueue 把任意 goroutine 上的活排给界面线程（Windows：跑消息循环的主线程；窗口、WebView2 与 COM 都只能在
// 那里碰）。平台层只提供 wake：往界面线程的消息队列里投一条消息，界面线程收到后调 drain。
//
// 界面线程还没起来时（托盘的 OnReady 可能比消息循环先跑）活先排着，start 时一起做；close 之后的活直接丢掉
// —— 窗口已经拆了，再做只会碰到已经释放的句柄。
type uiQueue struct {
	mu     sync.Mutex
	items  []func()
	wake   func() bool // 平台层的投递；返回假表示没投进去（窗口没了、消息队列满了）
	woken  bool        // 已经投过一条、界面线程还没来取：不用再投
	closed bool
	// onUI 报告当前 goroutine 是不是就在界面线程上（call 在界面线程上直接做，否则会等自己等到超时）
	onUI func() bool
}

var errUIClosed = errors.New("桌面窗口已经关闭")

// post 排一件活；队列已经关了返回假。
func (q *uiQueue) post(f func()) bool {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return false
	}
	q.items = append(q.items, f)
	wake := q.claimWake()
	q.mu.Unlock()
	q.fire(wake)
	return true
}

// claimWake：要投递就占住 woken 并返回投递办法（持锁调）。
func (q *uiQueue) claimWake() func() bool {
	if q.woken || q.wake == nil || len(q.items) == 0 {
		return nil
	}
	q.woken = true
	return q.wake
}

// fire 在锁外投递；没投进去就放开 woken，下一次 post 再试（不然排着的活永远没人叫）。
func (q *uiQueue) fire(wake func() bool) {
	if wake == nil || wake() {
		return
	}
	q.mu.Lock()
	q.woken = false
	q.mu.Unlock()
}

// start 在界面线程就绪时调：记下投递办法，有排着的活就叫一次。
func (q *uiQueue) start(wake func() bool) {
	q.mu.Lock()
	q.wake = wake
	w := q.claimWake()
	q.mu.Unlock()
	q.fire(w)
}

// drain 在界面线程上把排着的活全做掉（做的过程中新排进来的也算）。
func (q *uiQueue) drain() {
	for {
		q.mu.Lock()
		items := q.items
		q.items = nil
		q.woken = false
		q.mu.Unlock()
		if len(items) == 0 {
			return
		}
		for _, f := range items {
			f()
		}
	}
}

// close 之后 post 一律失败；还排着的活丢掉。
func (q *uiQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.items = nil
	q.wake = nil
	q.woken = false
	q.mu.Unlock()
}

// call 在界面线程上做 f 并等它做完，最多 timeout。超时时还没开始的 f 不会再做（已经放弃了）。
func (q *uiQueue) call(timeout time.Duration, f func()) error {
	q.mu.Lock()
	onUI, closed := q.onUI, q.closed
	q.mu.Unlock()
	if closed {
		return errUIClosed
	}
	if onUI != nil && onUI() {
		f()
		return nil
	}
	done := make(chan struct{})
	var mu sync.Mutex
	abandoned := false
	ok := q.post(func() {
		mu.Lock()
		skip := abandoned
		mu.Unlock()
		if skip {
			return
		}
		f()
		close(done)
	})
	if !ok {
		return errUIClosed
	}
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		mu.Lock()
		defer mu.Unlock()
		select {
		case <-done: // 恰好在超时那一刻做完了
			return nil
		default:
		}
		abandoned = true
		return errors.New("界面线程没有在限期内响应（窗口卡住了？）")
	}
}
