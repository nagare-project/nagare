package artcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 封面缓存的上限与淘汰。
//
// 发现页成了第一屏，一屏就是几百张海报，缓存不设上限就只涨不落。超过上限时从最久没用的封面开始删
// （「用」= 经 /art 转发一次，或拼接口响应时用到它的海报色；记账见 index.go），连同它的取色边车一起删。
//
// 媒体库认出作品的封面、落盘目录（离线时发现页靠它）用到的图，要等别的都删完了才轮到：
// 它们是离线时还能显示的那部分，删了就要等联网才回来。缓存自己不知道哪些封面「在用」——
// 那要看媒体库与目录磁盘层，而这两样都在 internal/api 里，artcache 不能反过来依赖它（会成环）。
// 所以由 main 接上一个回调（SetPinned），淘汰时问一次。
//
// 淘汰只在后台做（Run），不在请求路径上：下载落盘后发现超限只是发个信号。正在被 /art 读的文件（Open 到 Close）
// 与正在下载的文件（还是临时文件，没有改名到位）都不会被删。

const (
	// MaxBytes 是封面缓存的总上限（约 300MB）。封面一张 50–300KB，够存一两千部作品。
	MaxBytes int64 = 300 << 20
	// maxCacheEntries 是文件数上限：一堆很小的图也不至于把目录与索引撑得没边。
	maxCacheEntries = 20_000
	// evictTargetPercent：一旦超出，淘汰到上限的 90% 为止 —— 多腾出一截，
	// 不至于每下一张新封面就跑一轮（每轮都要问一次哪些封面在用，那一步要读盘）。
	evictTargetPercent = 90
	// maintainInterval 是后台维护的周期：刷索引、补查一次是否超限。
	maintainInterval = time.Minute
	// pinnedCacheTTL：「哪些封面在用」问一次记多久（见 pinnedNames）。
	pinnedCacheTTL = 30 * time.Second
)

// Usage 是封面缓存此刻的占用（设置页显示）。
type Usage struct {
	Bytes    int64 `json:"bytes"`
	Count    int   `json:"count"`
	MaxBytes int64 `json:"maxBytes"`
}

// Usage 返回当前占用。只读内存里的索引，不碰磁盘。
// 取色边车（每个几十字节）不计在内：这是给界面看的估计。
func (c *Cache) Usage() Usage {
	c.idx.mu.Lock()
	defer c.idx.mu.Unlock()
	return Usage{Bytes: c.idx.total, Count: len(c.idx.entries), MaxBytes: c.maxBytes}
}

// SetPinned 接上「最后才淘汰」的封面清单：fn 返回封面的原始地址。
// fn 只在后台淘汰时调用（不在请求路径上），可以读盘；启动时在 Run 之前调用一次。
func (c *Cache) SetPinned(fn func() []string) {
	c.pinMu.Lock()
	defer c.pinMu.Unlock()
	c.pinned = fn
	c.pinGen++
	c.pinCache = nil
}

// Run 是后台维护循环：超限时淘汰、定期刷索引；ctx 结束时最后刷一次索引再返回。
// 不调用 Run 的缓存（测试）只是不淘汰、不刷索引，取图与取色照常。
func (c *Cache) Run(ctx context.Context) {
	ticker := time.NewTicker(maintainInterval)
	defer ticker.Stop()
	// 上次运行留下的超限（上限改小了、或者上次退出前没来得及淘汰）
	c.evict(ctx)
	c.flushIndex(false)
	for {
		select {
		case <-ctx.Done():
			c.flushIndex(false)
			return
		case <-c.evictCh:
		case <-ticker.C:
		}
		c.evict(ctx)
		c.flushIndex(false)
	}
}

// requestEvict 通知后台该淘汰了；后台正忙时信号合并成一个，从不阻塞调用方。
func (c *Cache) requestEvict() {
	select {
	case c.evictCh <- struct{}{}:
	default:
	}
}

func (c *Cache) overLimitLocked() bool {
	return c.idx.total > c.maxBytes || len(c.idx.entries) > c.maxEntries
}

// evictCandidate 是淘汰顺序里的一项。used 是排序时看到的最近使用时间：
// 真去删的时候它变了，就说明刚被用过，这一轮放过它。
type evictCandidate struct {
	name   string
	used   int64
	pinned bool
}

// evict 超限时淘汰到目标以下。顺序：先不在用的、后在用的；各自从最久没用的开始。
func (c *Cache) evict(ctx context.Context) {
	c.idx.mu.Lock()
	over := c.overLimitLocked()
	c.idx.mu.Unlock()
	if !over {
		return
	}

	pinned := c.pinnedNames()
	order := c.evictionOrder(pinned)
	targetBytes := c.maxBytes * evictTargetPercent / 100
	targetCount := c.maxEntries * evictTargetPercent / 100
	var removed, removedPinned, failed int
	var freed int64
	var firstErr error
	for _, cand := range order {
		if ctx.Err() != nil || c.withinTarget(targetBytes, targetCount) {
			break
		}
		size, res, err := c.removeCover(cand.name, cand.used)
		switch res {
		case removeFailed:
			failed++
			if firstErr == nil {
				firstErr = err
			}
		case removeDone:
			removed++
			freed += size
			if cand.pinned {
				removedPinned++
			}
		}
	}
	if removed > 0 {
		log.Printf("artcache: 封面缓存超过上限，淘汰了 %d 张（%.1f MB），其中 %d 张是媒体库或离线目录在用的",
			removed, float64(freed)/(1<<20), removedPinned)
	}
	if failed > 0 {
		// 典型情况：Windows 上别的程序（杀毒、索引服务）正开着这个文件。留在索引里，下一轮再试。
		log.Printf("artcache: 有 %d 张封面淘汰时删不掉，下一轮再试：%v", failed, firstErr)
	}
}

// pinnedNames 问一次哪些封面在用，换算成缓存里的文件名。结果记 pinnedCacheTTL：
// 回调要逐个读离线目录的文件，删不掉的封面让每次下载都触发一轮淘汰时，不能每轮都重读一遍。
// 返回的集合只读。
func (c *Cache) pinnedNames() map[string]bool {
	c.pinMu.Lock()
	fn, gen, cached, at := c.pinned, c.pinGen, c.pinCache, c.pinAt
	c.pinMu.Unlock()
	if fn == nil {
		return map[string]bool{}
	}
	if cached != nil && c.now().Sub(at) < pinnedCacheTTL {
		return cached
	}
	names, ok := queryPinned(fn)
	if ok {
		c.pinMu.Lock()
		if c.pinGen == gen { // 期间换了回调就不记旧回调的结果
			c.pinCache, c.pinAt = names, c.now()
		}
		c.pinMu.Unlock()
	}
	return names
}

// queryPinned 调回调并换算成文件名。回调 panic 时当作没有（ok = false，不记下来），记一行日志：
// 后台 goroutine 里的 panic 会带走整个进程（包括正在放的片子）。
func queryPinned(fn func() []string) (names map[string]bool, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("artcache: 查询在用的封面时异常，这一轮按都不在用处理：%v", r)
			names, ok = map[string]bool{}, false
		}
	}()
	names = map[string]bool{}
	for _, src := range fn() {
		if src != "" {
			names[coverFileName(src)] = true
		}
	}
	return names, true
}

// coverFileName 是 Path 的文件名部分：<地址的 sha256>.img。
func coverFileName(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:]) + coverExt
}

// evictionOrder 按淘汰顺序排好当前索引的快照。
func (c *Cache) evictionOrder(pinned map[string]bool) []evictCandidate {
	c.idx.mu.Lock()
	order := make([]evictCandidate, 0, len(c.idx.entries))
	for name, e := range c.idx.entries {
		order = append(order, evictCandidate{name: name, used: e.used, pinned: pinned[name]})
	}
	c.idx.mu.Unlock()
	sort.Slice(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.pinned != b.pinned {
			return !a.pinned
		}
		if a.used != b.used {
			return a.used < b.used
		}
		return a.name < b.name
	})
	return order
}

func (c *Cache) withinTarget(targetBytes int64, targetCount int) bool {
	c.idx.mu.Lock()
	defer c.idx.mu.Unlock()
	return c.idx.total <= targetBytes && len(c.idx.entries) <= targetCount
}

// anyUse 传给 removeCover 表示不核对最近使用时间（清空时用）。
const anyUse int64 = -1

// removeResult 是 removeCover 的结果。
type removeResult int

const (
	removeDone   removeResult = iota // 删掉了
	removeGone                       // 已经不在索引里（别的淘汰 / 清空先删了）
	removeBusy                       // 正在被读，或排完序之后刚被用过：这一次放过
	removeFailed                     // 封面删不掉（err 非空）
)

// removeCover 删一张封面和它的取色边车并从索引里去掉。expectUsed 不是 anyUse 时，
// 使用时间与它不符（排完序之后刚被用过）就不删。封面删不掉时条目留在索引里、总量照算
// （不然它成了不受管的孤儿文件），下一轮再试。
func (c *Cache) removeCover(name string, expectUsed int64) (int64, removeResult, error) {
	path := filepath.Join(c.dir, name)
	c.idx.mu.Lock()
	e, found := c.idx.entries[name]
	if !found {
		c.idx.mu.Unlock()
		return 0, removeGone, nil
	}
	if c.idx.inUse[name] > 0 || (expectUsed != anyUse && e.used != expectUsed) {
		c.idx.mu.Unlock()
		return 0, removeBusy, nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		c.idx.mu.Unlock()
		return 0, removeFailed, fmt.Errorf("删除封面 %s: %w", name, err)
	}
	if err := os.Remove(path + toneSidecarSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// 封面已经删了：边车留下也只是几十字节，下次启动对账时清掉
		log.Printf("artcache: 删除取色结果失败（%s）：%v", name, err)
	}
	c.idx.total -= e.size
	delete(c.idx.entries, name)
	c.idx.dirty = true
	c.idx.mu.Unlock()

	c.forgetTone(path)
	return e.size, removeDone, nil
}

// Clear 清空封面缓存：删掉全部封面与取色结果。正在被 /art 读的那几张这一次留着，skipped 是它们
// 与删不掉的张数（界面据此提示「稍后再清一次」）。返回清完之后的占用。
func (c *Cache) Clear() (u Usage, skipped int) {
	c.idx.mu.Lock()
	names := make([]string, 0, len(c.idx.entries))
	for name := range c.idx.entries {
		names = append(names, name)
	}
	c.idx.mu.Unlock()

	var firstErr error
	for _, name := range names {
		_, res, err := c.removeCover(name, anyUse)
		if res == removeBusy || res == removeFailed {
			skipped++
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	skipped += c.sweepDir()
	if firstErr != nil {
		log.Printf("artcache: 清空封面缓存时有文件删不掉：%v", firstErr)
	}
	c.flushIndex(true)
	u = c.Usage()
	log.Printf("artcache: 已清空封面缓存（剩 %d 张，%d 张正在使用或删不掉）", u.Count, skipped)
	return u, skipped
}

// sweepDir 清掉目录里不在索引里的封面（别处直接写进来的）和没有主人的取色边车。返回删不掉的张数。
// 正在下载的是 .dl- 临时文件，不在这里动；改名到位与记账在同一把锁里，所以不会把刚下好的封面当成野文件。
func (c *Cache) sweepDir() (skipped int) {
	items, err := os.ReadDir(c.dir)
	if err != nil {
		log.Printf("artcache: 清空封面缓存时读不了目录：%v", err)
		return 0
	}
	for _, it := range items {
		name := it.Name()
		switch {
		case isCoverName(name):
			if !c.removeStray(name) {
				skipped++
			}
		case strings.HasSuffix(name, toneSidecarSuffix):
			c.removeOrphanSidecar(name)
		}
	}
	return skipped
}

// removeStray 删一张不在索引里、也没人在读的封面；返回 false 表示它留下了（正在读、或删不掉）。
// 在索引里的是快照之后刚下好的，不动，也不算留下。
func (c *Cache) removeStray(name string) bool {
	c.idx.mu.Lock()
	defer c.idx.mu.Unlock()
	if _, indexed := c.idx.entries[name]; indexed {
		return true
	}
	if c.idx.inUse[name] > 0 {
		return false
	}
	path := filepath.Join(c.dir, name)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("artcache: 删除封面失败（%s）：%v", name, err)
		return false
	}
	if err := os.Remove(path + toneSidecarSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("artcache: 删除取色结果失败（%s）：%v", name, err)
	}
	return true
}

// removeOrphanSidecar 删一个封面已经不在的取色边车。与写边车同一把锁，判断与删除之间不会有新封面插进来。
func (c *Cache) removeOrphanSidecar(sidecar string) {
	cover := filepath.Join(c.dir, strings.TrimSuffix(sidecar, toneSidecarSuffix))
	c.idx.mu.Lock()
	defer c.idx.mu.Unlock()
	if _, err := os.Stat(cover); !errors.Is(err, fs.ErrNotExist) {
		return
	}
	c.removeQuietly(sidecar, "没有对应封面的取色结果")
}

// forgetTone 去掉内存里一张封面的取色结果：封面删了，再下载回来时要重新算、重新写边车。
func (c *Cache) forgetTone(path string) {
	c.toneMu.Lock()
	defer c.toneMu.Unlock()
	delete(c.toneMemo, path)
}
