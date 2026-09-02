package mpv

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 构造即探测；Get 读到的就是那次结果。
func TestRuntime_InitialDetect(t *testing.T) {
	var calls atomic.Int32
	rt := NewRuntimeWith(func(explicit string) (Info, error) {
		calls.Add(1)
		assert.Equal(t, "/given/mpv", explicit)
		return Info{Path: explicit, Version: "0.41.0", Source: SourceExplicit}, nil
	}, "/given/mpv")

	info, err := rt.Get()
	require.NoError(t, err)
	assert.Equal(t, "/given/mpv", info.Path)
	assert.Equal(t, int32(1), calls.Load())
}

// 用户装好 mpv 后 Redetect：状态从「缺失」翻成「找到」，不需要重启。
func TestRuntime_RedetectUpdatesSharedState(t *testing.T) {
	installed := false
	rt := NewRuntimeWith(func(string) (Info, error) {
		if !installed {
			return Info{}, errors.New("未找到 mpv")
		}
		return Info{Path: "/opt/homebrew/bin/mpv", Version: "0.41.0", Source: SourceKnown}, nil
	}, "")

	_, err := rt.Get()
	require.ErrorContains(t, err, "未找到")

	installed = true
	info, err := rt.Redetect("")
	require.NoError(t, err)
	assert.Equal(t, SourceKnown, info.Source)

	got, err := rt.Get()
	require.NoError(t, err)
	assert.Equal(t, info, got)
}

// 并发 Get/Redetect 无数据竞争（配合 -race）。
func TestRuntime_ConcurrentAccess(t *testing.T) {
	rt := NewRuntimeWith(func(string) (Info, error) {
		return Info{Path: "/x/mpv", Version: "0.41.0"}, nil
	}, "")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = rt.Redetect("") }()
		go func() { defer wg.Done(); _, _ = rt.Get() }()
	}
	wg.Wait()

	info, err := rt.Get()
	require.NoError(t, err)
	assert.Equal(t, "/x/mpv", info.Path)
}

// detect 为 nil 时退回真实 Detect（这里只验证不崩，不断言宿主机有无 mpv）。
func TestRuntime_NilDetectFallsBack(t *testing.T) {
	injectDetect(t, detectFakes{goos: "linux"})
	rt := NewRuntimeWith(nil, "")
	_, err := rt.Get()
	require.ErrorContains(t, err, "未找到 mpv")
}
