package api

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 没有进行中的首次扫描时，waitInitialScan 立即返回（其余所有测试与单元都走这一条）。
func TestWaitInitialScanReturnsAtOnceWithoutBackgroundScan(t *testing.T) {
	s := &LibraryService{}
	start := time.Now()
	s.waitInitialScan(context.Background())
	assert.Less(t, time.Since(start), time.Second)
}

// 首次扫描进行中，等到它结束才返回；ctx 取消时也立即放手。
func TestWaitInitialScanBlocksUntilScanDone(t *testing.T) {
	pending := make(chan struct{})
	s := &LibraryService{initialScan: pending}

	returned := make(chan struct{})
	go func() {
		s.waitInitialScan(context.Background())
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("扫描没结束就返回了")
	case <-time.After(50 * time.Millisecond):
	}
	close(pending)
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("扫描结束后没有放行")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s2 := &LibraryService{initialScan: make(chan struct{})}
	start := time.Now()
	s2.waitInitialScan(ctx)
	require.Less(t, time.Since(start), time.Second)
}
