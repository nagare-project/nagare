package api

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// catalogFetchTimeout 是一次上游读取的独立期限（与发起它的页面无关）。
	catalogFetchTimeout = 20 * time.Second
	// refreshBackoff：后台刷新失败后，这么久之内不再重试 —— 断网时每次打开页面都去撞一次上游没有意义，
	// 上游限流时更不该接着撞。
	refreshBackoff = time.Minute
	// readCacheMaxEntries 是内存里最多保留的键数。
	readCacheMaxEntries = 128
)

type cachedReadValue struct {
	value     any
	expires   time.Time
	fetchedAt time.Time
	fromDisk  bool      // 从磁盘读回、还没被上游刷新过
	failedAt  time.Time // 最近一次后台刷新失败的时间；零 = 没有失败过
}

// cacheMeta 描述一次读取拿到的是哪一份数据，视图据此告诉界面「离线 · 更新于 …」。
type cacheMeta struct {
	fetchedAt time.Time
	fromDisk  bool // 来自磁盘（上次运行留下的）
	stale     bool // 超过缓存时限，后台正在刷新
	offline   bool // 最近一次向上游刷新失败，仍在显示上一份
}

type readResult struct {
	value any
	meta  cacheMeta
}

// readCache 的锁只保护内存状态；上游等待由按键 singleflight 合并，互不相关的请求不会串行。
//
// disk 不为 nil 时，白名单里的键（catalogDiskKey）多一层磁盘：先给已有的一份
// （内存里过期的，或盘上的），同时在后台刷新（stale-while-revalidate）；上游连不上就一直给这一份。
// 其余键（以及 disk 为 nil 的实例，如收藏）保持纯内存、过期即重取。
type readCache struct {
	mu      sync.Mutex
	entries map[string]cachedReadValue
	flights singleflight.Group
	now     func() time.Time
	disk    *catalogDisk
	bg      sync.WaitGroup // 在途的后台刷新（测试用它等刷新落定）
}

func newReadCache() *readCache {
	return &readCache{entries: map[string]cachedReadValue{}, now: time.Now}
}

func (c *readCache) persistent(key string) bool { return c.disk != nil && catalogDiskKey(key) }

// get 只认未过期的内存副本。
func (c *readCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v.value, ok && c.now().Before(v.expires)
}

// peek 取内存里的任何一份（过期的也算）：给详情、季度补放送倒计时用，
// 放送时间是绝对时刻，过期的快照照样能算出「下一集」。
// 没有磁盘层的键与以前一样只认未过期的。
func (c *readCache) peek(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v.value, ok && (c.persistent(key) || c.now().Before(v.expires))
}

// lookup 取内存副本：未过期的总能用；过期的只有可落盘的键才拿来先顶上（stale=true）。
func (c *readCache) lookup(key string) (readResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	if !ok {
		return readResult{}, false
	}
	stale := !c.now().Before(v.expires)
	if stale && !c.persistent(key) {
		return readResult{}, false
	}
	return readResult{v.value, cacheMeta{fetchedAt: v.fetchedAt, fromDisk: v.fromDisk, stale: stale, offline: !v.failedAt.IsZero()}}, true
}

func cachedRead[T any](ctx context.Context, c *readCache, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, error) {
	v, _, err := cachedReadMeta(ctx, c, key, ttl, load)
	return v, err
}

// cachedReadMeta 与 cachedRead 相同，另外报告这一份数据的来历（与数据本身在同一次读取里取得）。
func cachedReadMeta[T any](ctx context.Context, c *readCache, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, cacheMeta, error) {
	var zero T
	if r, ok := c.lookup(key); ok {
		if r.meta.stale {
			revalidate(c, key, ttl, load)
		}
		return r.value.(T), r.meta, nil
	}
	ch := c.flights.DoChan(key, func() (any, error) {
		if r, ok := c.lookup(key); ok {
			return r, nil
		}
		if c.persistent(key) {
			if r, ok := loadFromDisk[T](c, key, ttl); ok {
				return r, nil
			}
		}
		// 一个页面取消不会取消其他页面正在共用的同键请求；独立超时保证它不会永久驻留。
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), catalogFetchTimeout)
		defer cancel()
		value, err := load(fetchCtx)
		if err != nil {
			return nil, err
		}
		return readResult{value, c.put(key, value, ttl)}, nil
	})
	select {
	case <-ctx.Done():
		return zero, cacheMeta{}, ctx.Err()
	case result := <-ch:
		if result.Err != nil {
			// 内存副本被挤掉时，这次读取可能合并进了一次失败的后台刷新；盘上还有就先给盘上的
			if c.persistent(key) {
				if r, ok := loadFromDisk[T](c, key, ttl); ok {
					c.markFailed(key, result.Err) // 记下失败：接下来的读取按退避处理，并报 offline
					r.meta.offline = true
					return r.value.(T), r.meta, nil
				}
			}
			return zero, cacheMeta{}, result.Err
		}
		r := result.Val.(readResult)
		if r.meta.stale {
			revalidate(c, key, ttl, load)
		}
		return r.value.(T), r.meta, nil
	}
}

// loadFromDisk 把盘上的一份解码成 T 放进内存。解不成 T（上游结构变了而版本号没跟上）
// 与读不了的文件同样处理：丢掉，回到网络。
func loadFromDisk[T any](c *readCache, key string, ttl time.Duration) (readResult, bool) {
	raw, fetchedAt, ok := c.disk.load(key)
	if !ok {
		return readResult{}, false
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		c.disk.discard(key, "内容与当前结构不符")
		return readResult{}, false
	}
	now := c.now()
	expires := fetchedAt.Add(ttl)
	if fetchedAt.After(now) {
		expires = now // 时钟回拨过：来自「未来」的副本不能当新鲜的用上一整天
	}
	c.mu.Lock()
	c.insertLocked(key, cachedReadValue{value: value, expires: expires, fetchedAt: fetchedAt, fromDisk: true})
	c.mu.Unlock()
	return readResult{value, cacheMeta{fetchedAt: fetchedAt, fromDisk: true, stale: !now.Before(expires)}}, true
}

// put 记下刚从上游取回的一份；可落盘的键同时写盘。
func (c *readCache) put(key string, value any, ttl time.Duration) cacheMeta {
	now := c.now()
	c.mu.Lock()
	c.insertLocked(key, cachedReadValue{value: value, expires: now.Add(ttl), fetchedAt: now})
	c.mu.Unlock()
	if c.persistent(key) {
		c.disk.store(key, value, now)
	}
	return cacheMeta{fetchedAt: now}
}

// insertLocked 写入内存并守住条数上限：先清掉过期且不落盘的；还满就优先挤掉过期的。调用方持有 c.mu。
func (c *readCache) insertLocked(key string, v cachedReadValue) {
	now := c.now()
	for k, e := range c.entries {
		if !now.Before(e.expires) && !c.persistent(k) {
			delete(c.entries, k)
		}
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= readCacheMaxEntries {
		victim := ""
		for k, e := range c.entries {
			victim = k
			if !now.Before(e.expires) {
				break
			}
		}
		delete(c.entries, victim)
	}
	c.entries[key] = v
}

// revalidate 在后台刷新一个过期的键：与同键的其他读取合并成一次出站；
// 失败了留着旧的一份并标记 offline，refreshBackoff 之内不再重试。
func revalidate[T any](c *readCache, key string, ttl time.Duration, load func(context.Context) (T, error)) {
	c.mu.Lock()
	e, ok := c.entries[key]
	backoff := ok && !e.failedAt.IsZero() && c.now().Sub(e.failedAt) < refreshBackoff
	c.mu.Unlock()
	if backoff {
		return
	}
	c.bg.Add(1)
	ch := c.flights.DoChan(key, func() (any, error) {
		if r, ok := c.lookup(key); ok && !r.meta.stale {
			return r, nil // 排队期间别的请求已经刷新过
		}
		ctx, cancel := context.WithTimeout(context.Background(), catalogFetchTimeout)
		defer cancel()
		value, err := load(ctx)
		if err != nil {
			c.markFailed(key, err)
			return nil, err
		}
		return readResult{value, c.put(key, value, ttl)}, nil
	})
	go func() {
		defer c.bg.Done()
		<-ch
	}()
}

// markFailed 记下后台刷新失败。只在「从好到坏」时记一行日志，断网期间不刷屏。
func (c *readCache) markFailed(key string, err error) {
	c.mu.Lock()
	e, ok := c.entries[key]
	first := ok && e.failedAt.IsZero()
	if ok {
		e.failedAt = c.now()
		c.entries[key] = e
	}
	c.mu.Unlock()
	if first {
		// 不记 URL：上游错误里可能带着请求地址（见 art.go 的 withoutURL）
		log.Printf("目录：后台刷新 %s 失败，继续显示 %s 取到的上一份：%v", key, e.fetchedAt.UTC().Format(time.RFC3339), withoutURL(err))
	}
}

// waitBackground 等所有在途的后台刷新结束（测试用）。
func (c *readCache) waitBackground() { c.bg.Wait() }

// freshnessOf 合并几份数据的来历：只要有一份来自磁盘、过期或离线就报出来，
// 时间取其中最旧的一份（界面说「更新于 …」宁可说旧不说新）。没取到的板块（fetchedAt 为零）不算。
func freshnessOf(metas ...cacheMeta) *Freshness {
	var oldest time.Time
	notable, stale, offline := false, false, false
	for _, m := range metas {
		if m.fetchedAt.IsZero() {
			continue
		}
		if oldest.IsZero() || m.fetchedAt.Before(oldest) {
			oldest = m.fetchedAt
		}
		notable = notable || m.fromDisk || m.stale || m.offline
		stale = stale || m.stale
		offline = offline || m.offline
	}
	if !notable {
		return nil
	}
	return &Freshness{FetchedAt: oldest.UTC().Format(time.RFC3339), Stale: stale, Offline: offline}
}
