package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-time.After(50 * time.Millisecond):
		return false
	}
}

// 「收尾完成」要等封面缓存刷完索引：macOS ⌘Q 时系统在 Done 关闭后就结束进程，不会再走到 run 的末尾。
func TestAfterArtCacheWaitsForBoth(t *testing.T) {
	stopped, art := make(chan struct{}), make(chan struct{})
	all := afterArtCache(stopped, art)

	close(art)
	assert.False(t, closed(all), "播放等还没停好")
	close(stopped)
	assert.True(t, closed(all))

	stopped2, art2 := make(chan struct{}), make(chan struct{})
	all2 := afterArtCache(stopped2, art2)
	close(stopped2)
	assert.False(t, closed(all2), "索引还没刷完")
	close(art2)
	assert.True(t, closed(all2))
}
