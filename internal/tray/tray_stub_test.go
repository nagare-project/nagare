//go:build !((darwin && cgo) || windows)

package tray

import (
	"context"
	"testing"
	"time"
)

// 桩实现：ctx 取消后 Run 立刻返回，不调任何回调。
func TestStubRunReturnsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Run(ctx, Options{
			OnOpen: func() { t.Error("桩不应触发 OnOpen") },
			OnQuit: func() { t.Error("桩不应触发 OnQuit") },
		})
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("ctx 未取消 Run 不应返回")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 Run 没有返回")
	}
}
