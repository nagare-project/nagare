//go:build (darwin && cgo) || windows

package tray

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	go dispatch(ctx, Options{OnOpen: func() { opens <- struct{}{} }}, openCh, quitCh, func() { quits <- struct{}{} })

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
		openCh, quitCh, func() { order = append(order, "quit"); close(done) })

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
		openCh, quitCh, func() { quits <- struct{}{} })

	cancel()
	waitFor(t, func() bool { return len(quits) == 1 }, "ctx 取消后应收起图标让 Run 返回")
}

// 回调为 nil 不得 panic（Options 的可选字段）。
func TestDispatchNilCallbacks(t *testing.T) {
	openCh, quitCh := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go dispatch(context.Background(), Options{}, openCh, quitCh, func() { close(done) })

	openCh <- struct{}{}
	quitCh <- struct{}{}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("nil 回调下 dispatch 卡住了")
	}
}
