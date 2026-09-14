//go:build (darwin && cgo) || windows || linux

package tray

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withGrace 临时把终止宽限期调短。
func withGrace(t *testing.T, d time.Duration, f func()) {
	t.Helper()
	prev := terminateGrace
	terminateGrace = d
	t.Cleanup(func() { terminateGrace = prev })
	f()
}

// noReply 是非 macOS-Dock 场景下的 replyTerminate：永远不该被调到。
func noReply() { panic("没有系统终止请求却答复了系统") }

// waitFor 等一个条件成立，超时即失败。
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	require.Eventually(t, cond, 2*time.Second, 5*time.Millisecond, msg)
}

// 点「打开界面」只调 OnOpen，不退出；可以点多次。
func TestDispatchOpenDoesNotQuit(t *testing.T) {
	openCh, quitCh := make(chan struct{}), make(chan struct{})
	opens := make(chan struct{}, 4)
	quits := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatch(ctx, Options{OnOpen: func() { opens <- struct{}{} }}, openCh, quitCh, nil, func() { quits <- struct{}{} }, noReply)

	for i := 0; i < 3; i++ {
		openCh <- struct{}{}
	}
	waitFor(t, func() bool { return len(opens) == 3 }, "三次点击应触发三次 OnOpen")
	assert.Empty(t, quits, "打开界面不应触发退出")
}

// 点「退出 nagare」：先调 OnQuit，再调 quit（收起图标让 Run 返回）。
func TestDispatchQuitCallsOnQuitBeforeQuit(t *testing.T) {
	openCh, quitCh := make(chan struct{}), make(chan struct{})
	var order []string
	done := make(chan struct{})
	go dispatch(context.Background(), Options{OnQuit: func() { order = append(order, "onQuit") }},
		openCh, quitCh, nil, func() { order = append(order, "quit"); close(done) }, noReply)

	quitCh <- struct{}{}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("退出后没有调用 quit")
	}
	assert.Equal(t, []string{"onQuit", "quit"}, order, "必须先回写进度的 OnQuit，再收图标")
}

// ctx 取消（信号 / POST /api/shutdown）：收起图标，但不重复调 OnQuit。
func TestDispatchCancelQuitsWithoutOnQuit(t *testing.T) {
	openCh, quitCh := make(chan struct{}), make(chan struct{})
	quits := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go dispatch(ctx, Options{OnQuit: func() { t.Error("ctx 取消不应再调 OnQuit（主进程已在收尾）") }},
		openCh, quitCh, nil, func() { quits <- struct{}{} }, noReply)

	cancel()
	waitFor(t, func() bool { return len(quits) == 1 }, "ctx 取消后应收起图标让 Run 返回")
}

// 回调为 nil 不得 panic（Options 的可选字段）。
func TestDispatchNilCallbacks(t *testing.T) {
	openCh, quitCh := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go dispatch(context.Background(), Options{}, openCh, quitCh, nil, func() { close(done) }, noReply)

	openCh <- struct{}{}
	quitCh <- struct{}{}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("nil 回调下 dispatch 卡住了")
	}
}

// 系统请求终止（macOS ⌘Q / 注销 / 关机）：先 OnQuit，等收尾完成，再答复系统；
// 不走 quit（那条路会让 Run 返回，而系统此刻在等答复）。
func TestDispatchTerminateWaitsForTeardownThenReplies(t *testing.T) {
	openCh, quitCh, termCh := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	done := make(chan struct{})
	var order []string
	replied := make(chan struct{})
	go dispatch(context.Background(),
		Options{OnQuit: func() { order = append(order, "onQuit") }, Done: done},
		openCh, quitCh, termCh,
		func() { t.Error("终止路径不应调 quit") },
		func() { order = append(order, "reply"); close(replied) })

	termCh <- struct{}{}
	select {
	case <-replied:
		t.Fatal("收尾还没完成就答复了系统")
	case <-time.After(50 * time.Millisecond):
	}
	close(done)
	select {
	case <-replied:
	case <-time.After(2 * time.Second):
		t.Fatal("收尾完成后没有答复系统")
	}
	assert.Equal(t, []string{"onQuit", "reply"}, order)
}

// Done 为 nil（主进程没有可等的收尾）时立即答复。
func TestDispatchTerminateWithoutDoneRepliesImmediately(t *testing.T) {
	openCh, quitCh, termCh := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	replied := make(chan struct{})
	go dispatch(context.Background(), Options{}, openCh, quitCh, termCh,
		func() { t.Error("终止路径不应调 quit") }, func() { close(replied) })

	termCh <- struct{}{}
	select {
	case <-replied:
	case <-time.After(2 * time.Second):
		t.Fatal("没有答复系统")
	}
}

// 收尾卡死不能把「退出」也卡死：超过宽限期照样答复。
func TestWaitTeardownGivesUpAfterGrace(t *testing.T) {
	never := make(chan struct{})
	start := time.Now()
	withGrace(t, 30*time.Millisecond, func() { waitTeardown(never) })
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 30*time.Millisecond)
	assert.Less(t, elapsed, 2*time.Second)
}
