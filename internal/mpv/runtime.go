package mpv

import "sync"

// Runtime 是 mpv 探测结果的并发安全共享状态：API 的设置页与播放编排都从这里读。
// 用户按指引装好 mpv 后调用 Redetect 即时刷新，不必重启 nagare。
type Runtime struct {
	detect func(explicit string) (Info, error)

	// detectMu 串行化探测本身（外部命令，不能并发跑两份互相覆盖）；
	// mu 只保护结果字段的快读快写。
	detectMu sync.Mutex
	mu       sync.RWMutex
	info     Info
	err      error
}

// NewRuntime 用 Detect 立即探测一次并返回可共享的状态。
func NewRuntime(explicit string) *Runtime { return NewRuntimeWith(Detect, explicit) }

// NewRuntimeWith 用自定义探测函数构造（测试注入）；detect 为 nil 时退回 Detect。
func NewRuntimeWith(detect func(explicit string) (Info, error), explicit string) *Runtime {
	if detect == nil {
		detect = Detect
	}
	r := &Runtime{detect: detect}
	_, _ = r.Redetect(explicit) // 结果已存进状态，调用方用 Get 读取
	return r
}

// Get 返回最近一次探测的结果。
func (r *Runtime) Get() (Info, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.info, r.err
}

// Redetect 重新探测并更新共享状态，返回本次结果。
func (r *Runtime) Redetect(explicit string) (Info, error) {
	r.detectMu.Lock()
	defer r.detectMu.Unlock()

	info, err := r.detect(explicit)
	r.mu.Lock()
	r.info, r.err = info, err
	r.mu.Unlock()
	return info, err
}
