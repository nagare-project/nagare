package api

import (
	"context"
	"golang.org/x/sync/singleflight"
	"sync"
	"time"
)

type cachedReadValue struct {
	value     any
	expires   time.Time
	fetchedAt time.Time
}

// readCache 的锁只保护内存状态；上游等待由按键 singleflight 合并，互不相关的请求不会串行。
type readCache struct {
	mu      sync.Mutex
	entries map[string]cachedReadValue
	flights singleflight.Group
	now     func() time.Time
}

func newReadCache() *readCache {
	return &readCache{entries: map[string]cachedReadValue{}, now: time.Now}
}
func (c *readCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v.value, ok && c.now().Before(v.expires)
}
func cachedRead[T any](ctx context.Context, c *readCache, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, error) {
	if value, ok := c.get(key); ok {
		return value.(T), nil
	}
	ch := c.flights.DoChan(key, func() (any, error) {
		if value, ok := c.get(key); ok {
			return value, nil
		}
		// 一个页面取消不会取消其他页面正在共用的同键请求；独立超时保证它不会永久驻留。
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		value, err := load(fetchCtx)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for k, v := range c.entries {
			if !c.now().Before(v.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= 128 {
			for k := range c.entries {
				delete(c.entries, k)
				break
			}
		}
		c.entries[key] = cachedReadValue{value: value, expires: c.now().Add(ttl), fetchedAt: c.now()}
		return value, nil
	})
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case result := <-ch:
		if result.Err != nil {
			var zero T
			return zero, result.Err
		}
		return result.Val.(T), nil
	}
}

// fetchedAt 返回真正取回上游数据的时间，不把缓存命中说成刚刚刷新。
func (c *readCache) fetchedAt(key string) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries[key].fetchedAt
}
