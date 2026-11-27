package api

import (
	"context"
	"time"
)

// initialScanWait 是 GET /api/library 最多等首次后台扫描多久。
// 发现页等不依赖媒体库的页面不经过这里，窗口与服务可以先起来；只有打开媒体库页时才可能等。
const initialScanWait = 20 * time.Second

// RescanInBackground 在后台跑首次扫描（启动时用）：服务与桌面窗口不必等几百个视频扫完。
// 扫描进行中 GET /api/library 会等它做完（见 waitInitialScan），页面不会先闪一下「没有文件夹 / 没有视频」。
// done 在扫描结束后调用（可为 nil），只在调用方的 goroutine 里跑一次。
func (s *LibraryService) RescanInBackground(done func(Stats)) {
	pending := make(chan struct{})
	s.mu.Lock()
	s.initialScan = pending
	s.mu.Unlock()
	go func() {
		stats := s.Rescan()
		close(pending)
		if done != nil {
			done(stats)
		}
	}()
}

// waitInitialScan 在首次后台扫描进行中时等它结束（最多 initialScanWait，ctx 取消也立即返回）；
// 没有进行中的首次扫描时马上返回。
func (s *LibraryService) waitInitialScan(ctx context.Context) {
	s.mu.RLock()
	pending := s.initialScan
	s.mu.RUnlock()
	if pending == nil {
		return
	}
	timer := time.NewTimer(initialScanWait)
	defer timer.Stop()
	select {
	case <-pending:
	case <-ctx.Done():
	case <-timer.C:
	}
}
