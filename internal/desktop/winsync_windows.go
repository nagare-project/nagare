//go:build windows

package desktop

// 宿主窗口变过之后与 mpv 对一遍全屏 / 最大化 / 置顶（规则在 winstate.go）：mpv 的属性通知会合并，宿主写过去的值与
// 紧跟着的另一次改动常常一个通知都不发，所以不能只靠通知。全部在界面线程上，读 mpv 的那几下放在后台 goroutine 里。

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	// 宿主的窗口变过之后，隔这么久看一次 mpv 的子窗口跟上客户区没有、mpv 把宿主写的值报回来没有
	childSettlePoll = 100 * time.Millisecond
	// 一轮最多等这么久（mpv 卡住、子窗口没了时不一直读下去）
	childSettleLimit = 3 * time.Second
)

// reconciledFlags 是读回来对的属性：宿主会写过去的那三个。window-minimized 不在里面 —— 它是一次性的请求（宿主
// 照做之后写回假），按「读到真就最小化」对，写回假那一下要是丢了，用户每还原一次窗口就又被最小化一次。
var reconciledFlags = []string{"fullscreen", "window-maximized", "ontop"}

// reconcileState 是一个会话里「读回来对」的进度（界面线程）。
type reconcileState struct {
	settleGen uint64    // settleChild 的轮次：只有最新的一轮往下走
	reading   bool      // 正有一次读在路上（同一时间只有一次，mpv 卡住时不会越积越多）
	again     bool      // 读在路上时又要求了一次：回来之后马上再读
	until     time.Time // 读到这个时候为止（还在等 mpv 报回来时）
}

// settleChild（界面线程）：宿主的窗口刚变过（改大小、全屏、置顶），或刚写过值给 mpv。先等 mpv 的子窗口跟上客户区
// （连续两次看到同样大小：它多半处理完了那次 WM_SIZE，不会再有一个假晚到），再读回来对（requestReconcile）。
// 只留最新的一轮：拖着窗口边框改大小时每个 WM_SIZE 都会来一次。等到时限还没跟上（mpv 卡住、子窗口没了）也照样读。
func (a *app) settleChild(s *videoSession) {
	s.rec.settleGen++
	gen, deadline := s.rec.settleGen, time.Now().Add(childSettleLimit)
	var poll func(seen bool)
	poll = func(seen bool) {
		a.after(childSettlePoll, func() {
			if a.session != s || s.rec.settleGen != gen {
				return // 会话换了，或者窗口又变了一次（新的一轮接手）
			}
			child := s.child.Load()
			want, got := clientRect(a.videoHost), clientRect(child)
			settled := child != 0 && got.width() == want.width() && got.height() == want.height()
			switch {
			case settled && seen:
				a.requestReconcile(s)
			case time.Now().After(deadline):
				traceWin("mpv 子窗口 %s 内没跟上（%dx%d，客户区 %dx%d）", childSettleLimit, got.width(), got.height(), want.width(), want.height())
				a.requestReconcile(s)
			default:
				poll(settled)
			}
		})
	}
	poll(false)
}

// requestReconcile（界面线程）：读回来对一遍；之后只要 mpv 还没把宿主认为的值报回来（windowSync.pending），
// 就隔一会儿再读，直到报回来或到时限。
func (a *app) requestReconcile(s *videoSession) {
	s.rec.until = time.Now().Add(childSettleLimit)
	if s.rec.reading {
		s.rec.again = true
		return
	}
	a.readFlags(s)
}

// readFlags（界面线程）：在后台读 reconciledFlags（与宿主的写走同一条连接、按顺序执行，读到的是写过之后的值），
// 读完回到界面线程对。读不到（mpv 卡住、连接断了）就停在那里，不往下读。
func (a *app) readFlags(s *videoSession) {
	c := s.ipc.Load()
	if c == nil || s.sync == nil {
		return
	}
	s.rec.reading = true
	marks := s.sync.marks(reconciledFlags)
	go func() {
		vals := make([]*bool, len(reconciledFlags))
		var readErr error
		for i, name := range reconciledFlags {
			raw, err := c.request(ipcReplyWait, "get_property", name)
			var v bool
			if err == nil {
				err = json.Unmarshal(raw, &v)
			}
			if err != nil {
				readErr = fmt.Errorf("读 %s：%w", name, err)
				break
			}
			vals[i] = &v
		}
		a.q.post(func() { a.flagsRead(s, marks, vals, readErr) })
	}()
}

// flagsRead（界面线程）：按宿主此刻的状态对读回来的值，然后决定还要不要再读。
func (a *app) flagsRead(s *videoSession, marks []int, vals []*bool, readErr error) {
	s.rec.reading = false
	if a.session != s || s.sync == nil {
		return
	}
	for i, name := range reconciledFlags {
		if vals[i] == nil {
			continue
		}
		host := a.hostState()
		acts, writes := s.sync.reconcile(name, *vals[i], host, marks[i])
		if len(acts) > 0 || len(writes) > 0 {
			traceWin("读回 %s=%v（宿主 %+v）→ 动作 %v、写回 %v", name, *vals[i], host, acts, writes)
		}
		s.write(writes)
		a.apply(acts)
	}
	if readErr != nil {
		traceWin("读回来对没做完：%v", readErr)
		s.rec.again = false
		return
	}
	switch {
	case s.rec.again:
		s.rec.again = false
		a.readFlags(s)
	case s.sync.pending(reconciledFlags) && time.Now().Before(s.rec.until):
		a.after(childSettlePoll, func() {
			if a.session == s && !s.rec.reading {
				a.readFlags(s)
			}
		})
	case s.sync.pending(reconciledFlags):
		traceWin("mpv %s 内没把宿主写的值报回来", childSettleLimit)
	}
}

// after 隔 d 之后在界面线程上跑 f（队列关了就不跑）。
func (a *app) after(d time.Duration, f func()) {
	time.AfterFunc(d, func() { a.q.post(f) })
}

// traceWin 记一条窗口状态同步的事件：只有测试构建设了 testHooks.trace（失败时连同状态一起报出来），正式构建什么都不做。
func traceWin(format string, args ...any) {
	if t := testHooks.trace; t != nil {
		t(fmt.Sprintf(format, args...))
	}
}
