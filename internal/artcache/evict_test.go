package artcache

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock 是可以手动拨的钟：淘汰顺序按最近使用时间排，测试要它确定。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_800_000_000, 0)} }

func (f *fakeClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) tick() {
	f.mu.Lock()
	f.t = f.t.Add(time.Second)
	f.mu.Unlock()
}

// newCappedCache 建一个上限很小、钟可控的缓存（只走本地文件，不需要网络）。
func newCappedCache(t *testing.T, dir string, maxBytes int64) (*Cache, *fakeClock) {
	t.Helper()
	c, err := New(dir)
	require.NoError(t, err)
	t.Cleanup(c.toneWG.Wait)
	clk := newFakeClock()
	c.now = clk.now
	c.maxBytes = maxBytes
	return c, clk
}

func coverURL(i int) string { return fmt.Sprintf("https://img.example/cover/%03d.jpg", i) }

// putCover 走与下载同一条落盘路径（临时文件 → commitFile），钟先走一秒。
func putCover(t *testing.T, c *Cache, clk *fakeClock, rawURL string, size int) string {
	t.Helper()
	require.NoError(t, commitCover(c, clk, rawURL, size))
	return c.Path(rawURL)
}

// commitCover 是 putCover 不带断言的一半（并发测试里在别的 goroutine 上用，那里不能调 FailNow）。
func commitCover(c *Cache, clk *fakeClock, rawURL string, size int) error {
	clk.tick()
	tmp, err := os.CreateTemp(c.dir, dlTempPrefix+"*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write([]byte(strings.Repeat("x", size)))
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	return c.commitFile(tmp.Name(), c.Path(rawURL), int64(size))
}

// newImageServer 起一个总是回 pngBytes 的图片服务。
func newImageServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// putSidecar 给封面放一个取色结果。
func putSidecar(t *testing.T, coverPath string) {
	t.Helper()
	require.NoError(t, os.WriteFile(coverPath+toneSidecarSuffix, []byte(`{"version":1,"hue":200}`), 0o600))
}

// cachedURLs 列出还在盘上的封面（按编号）。
func cachedURLs(t *testing.T, c *Cache, n int) []int {
	t.Helper()
	var out []int
	for i := 0; i < n; i++ {
		if _, err := os.Stat(c.Path(coverURL(i))); err == nil {
			out = append(out, i)
		}
	}
	return out
}

func TestEvictRemovesLeastRecentlyUsedFirst(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), 1000)
	for i := 0; i < 12; i++ { // 12 × 100 = 1200，超出 200
		putCover(t, c, clk, coverURL(i), 100)
	}
	// 最早的两张刚被用过：不该被淘汰
	clk.tick()
	c.touch(c.Path(coverURL(0)))
	f, err := c.openTracked(c.Path(coverURL(1)))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	c.evict(context.Background())

	// 淘汰到上限的 90%（900 字节）：去掉最久没用的 3 张（2、3、4）
	assert.Equal(t, []int{0, 1, 5, 6, 7, 8, 9, 10, 11}, cachedURLs(t, c, 12))
	u := c.Usage()
	assert.EqualValues(t, 900, u.Bytes)
	assert.Equal(t, 9, u.Count)
	assert.Equal(t, int64(1000), u.MaxBytes)
}

func TestEvictDoesNothingUnderLimit(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), 1000)
	for i := 0; i < 10; i++ {
		putCover(t, c, clk, coverURL(i), 100)
	}
	c.evict(context.Background())
	assert.Len(t, cachedURLs(t, c, 10), 10, "正好到上限不淘汰")
}

func TestEvictHonoursEntryCountLimit(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), 1<<20)
	c.maxEntries = 10
	for i := 0; i < 11; i++ {
		putCover(t, c, clk, coverURL(i), 10)
	}
	c.evict(context.Background())
	assert.Equal(t, []int{2, 3, 4, 5, 6, 7, 8, 9, 10}, cachedURLs(t, c, 11), "文件数也按 90% 腾")
}

// 媒体库 / 离线目录在用的封面最后才删：先删完别的，还超就从在用的里最久没用的开始。
func TestEvictKeepsPinnedCoversUntilLast(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), 1000)
	for i := 0; i < 12; i++ {
		putCover(t, c, clk, coverURL(i), 100)
	}
	// 最久没用的四张都在用（0–3），外加一个空地址和一个不在缓存里的地址（都应当无害）
	c.SetPinned(func() []string {
		return []string{coverURL(0), coverURL(1), coverURL(2), coverURL(3), "", "https://img.example/none.jpg"}
	})
	c.evict(context.Background())
	assert.Equal(t, []int{0, 1, 2, 3, 7, 8, 9, 10, 11}, cachedURLs(t, c, 12), "先删不在用的 4、5、6")

	// 上限再收紧：不在用的删光了还不够，才轮到在用的（最久没用的先走）
	c.maxBytes = 300
	c.evict(context.Background())
	assert.Equal(t, []int{2, 3}, cachedURLs(t, c, 12))
}

// 在用清单的回调 panic 不能带走进程：这一轮按都不在用处理。
func TestEvictSurvivesPinnedPanic(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), 1000)
	for i := 0; i < 11; i++ {
		putCover(t, c, clk, coverURL(i), 100)
	}
	c.SetPinned(func() []string { panic("boom") })
	require.NotPanics(t, func() { c.evict(context.Background()) })
	assert.Equal(t, []int{2, 3, 4, 5, 6, 7, 8, 9, 10}, cachedURLs(t, c, 11))
}

func TestEvictRemovesSidecarWithCover(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), 1000)
	for i := 0; i < 11; i++ {
		putSidecar(t, putCover(t, c, clk, coverURL(i), 100))
	}
	c.rememberTone(c.Path(coverURL(0)), toneResult{})
	c.evict(context.Background())

	for _, gone := range []int{0, 1} {
		assert.NoFileExists(t, c.Path(coverURL(gone)))
		assert.NoFileExists(t, c.Path(coverURL(gone))+toneSidecarSuffix, "淘汰封面时边车一起删")
	}
	assert.FileExists(t, c.Path(coverURL(2))+toneSidecarSuffix)
	_, known := c.memoTone(c.Path(coverURL(0)))
	assert.False(t, known, "内存里的取色结果也要忘掉，再下载回来时重新算")
}

// 正在被 /art 读的文件不删；读完之后下一轮照常淘汰。排完序之后刚被用过的也放过。
func TestEvictSkipsCoversInUse(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), 1000)
	for i := 0; i < 11; i++ {
		putCover(t, c, clk, coverURL(i), 100)
	}
	// 排完序之后刚被用过：放过
	order := c.evictionOrder(nil)
	clk.tick()
	c.touch(c.Path(coverURL(0)))
	_, res, err := c.removeCover(order[0].name, order[0].used)
	require.NoError(t, err)
	assert.Equal(t, removeBusy, res, "排序时看到的使用时间已经变了")

	f, err := c.openTracked(c.Path(coverURL(0)))
	require.NoError(t, err)
	c.idx.mu.Lock()
	c.idx.entries[order[0].name] = coverEntry{size: 100, used: 0} // 让它重新排到最前
	c.idx.mu.Unlock()

	c.evict(context.Background())
	assert.FileExists(t, c.Path(coverURL(0)), "正在读的封面不删")
	assert.Equal(t, []int{0, 3, 4, 5, 6, 7, 8, 9, 10}, cachedURLs(t, c, 11))

	require.NoError(t, f.Close())
	_ = f.Close() // 重复 Close：文件那边报「已关闭」，正在使用的计数不能被减两次
	c.idx.mu.Lock()
	assert.Empty(t, c.idx.inUse)
	c.idx.mu.Unlock()
	c.maxBytes = 800
	c.evict(context.Background())
	assert.NoFileExists(t, c.Path(coverURL(0)), "读完就能淘汰了")
}

func TestClearRemovesEverythingButFilesInUse(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), MaxBytes)
	for i := 0; i < 5; i++ {
		putSidecar(t, putCover(t, c, clk, coverURL(i), 100))
	}
	// 别处直接写进目录的封面（不在索引里）与没有主人的边车，也一并清掉
	stray := c.Path(coverURL(7))
	require.NoError(t, os.WriteFile(stray, []byte("stray"), 0o600))
	putSidecar(t, c.Path(coverURL(8)))
	// 正在下载的临时文件不动
	dl := filepath.Join(c.dir, dlTempPrefix+"inflight")
	require.NoError(t, os.WriteFile(dl, []byte("partial"), 0o600))

	f, err := c.openTracked(c.Path(coverURL(4)))
	require.NoError(t, err)
	u, skipped := c.Clear()
	assert.Equal(t, 1, skipped, "正在读的那张留着")
	assert.Equal(t, 1, u.Count)
	assert.EqualValues(t, 100, u.Bytes)
	require.NoError(t, f.Close())

	u, skipped = c.Clear()
	assert.Zero(t, skipped)
	assert.Zero(t, u.Count)
	assert.Zero(t, u.Bytes)
	assert.Equal(t, []string{dlTempPrefix + "inflight", indexFileName}, dirNames(t, c.dir))
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	items, err := os.ReadDir(dir)
	require.NoError(t, err)
	var out []string
	for _, it := range items {
		out = append(out, it.Name())
	}
	sort.Strings(out)
	return out
}

func TestIndexSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	c, clk := newCappedCache(t, dir, 1000)
	for i := 0; i < 11; i++ {
		putCover(t, c, clk, coverURL(i), 100)
	}
	clk.tick()
	c.touch(c.Path(coverURL(0))) // 最早的一张刚被用过
	c.flushIndex(false)

	// 重启：mtime 全部改成同一时刻 —— 顺序只能来自 index.json
	same := time.Unix(1_700_000_000, 0)
	for i := 0; i < 11; i++ {
		require.NoError(t, os.Chtimes(c.Path(coverURL(i)), same, same))
	}
	c2, _ := newCappedCache(t, dir, 1000)
	assert.Equal(t, c.Usage(), c2.Usage())
	c2.evict(context.Background())
	assert.Equal(t, []int{0, 3, 4, 5, 6, 7, 8, 9, 10}, cachedURLs(t, c2, 11))
}

// 索引坏了（或版本不对）：按目录重建，最近使用按 mtime；残留临时文件、空文件、没有主人的边车顺手删掉。
func TestCorruptIndexRebuildsFromDirectory(t *testing.T) {
	for name, content := range map[string]string{
		"损坏":   "{not json",
		"版本不符": `{"version":99,"used":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			base := time.Unix(1_700_000_000, 0)
			for i := 0; i < 4; i++ {
				p := filepath.Join(dir, coverFileName(coverURL(i)))
				require.NoError(t, os.WriteFile(p, []byte(strings.Repeat("x", 100)), 0o600))
				mt := base.Add(time.Duration(10-i) * time.Minute) // 0 最新、3 最旧
				require.NoError(t, os.Chtimes(p, mt, mt))
			}
			putSidecar(t, filepath.Join(dir, coverFileName(coverURL(0))))
			putSidecar(t, filepath.Join(dir, coverFileName(coverURL(9)))) // 没有主人
			require.NoError(t, os.WriteFile(filepath.Join(dir, coverFileName(coverURL(5))), nil, 0o600))
			for _, tmp := range []string{dlTempPrefix + "1", toneTempPrefix + "2", indexTempPrefix + "3"} {
				require.NoError(t, os.WriteFile(filepath.Join(dir, tmp), []byte("x"), 0o600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("keep"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, indexFileName), []byte(content), 0o600))

			c, _ := newCappedCache(t, dir, 350)
			assert.Equal(t, Usage{Bytes: 400, Count: 4, MaxBytes: 350}, c.Usage())
			names := dirNames(t, dir)
			assert.NotContains(t, names, coverFileName(coverURL(5)), "空文件删掉")
			assert.NotContains(t, names, coverFileName(coverURL(9))+toneSidecarSuffix)
			assert.Contains(t, names, coverFileName(coverURL(0))+toneSidecarSuffix)
			assert.Contains(t, names, "unrelated.txt", "不认识的文件不动")
			for _, n := range names {
				assert.False(t, strings.HasPrefix(n, "."), "残留临时文件 %s 应当删掉", n)
			}

			c.evict(context.Background()) // 目标 315 字节：删掉最旧的一张就够
			assert.Equal(t, []int{0, 1, 2}, cachedURLs(t, c, 4), "重建后按 mtime 排：最旧的先走")

			c.flushIndex(false) // 重建过的索引是脏的，会写回一份合法的
			c3, err := New(dir)
			require.NoError(t, err)
			assert.Equal(t, 3, c3.Usage().Count)
			used, ok := c3.readIndexFile()
			require.True(t, ok)
			assert.Len(t, used, 3)
		})
	}
}

// 封面在取色期间被淘汰：不写边车（否则留下没有主人的边车），也不把结果记进内存。
func TestToneSidecarNotWrittenForEvictedCover(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), MaxBytes)
	path := putCover(t, c, clk, coverURL(0), 100)
	_, res, err := c.removeCover(coverFileName(coverURL(0)), anyUse)
	require.NoError(t, err)
	require.Equal(t, removeDone, res)

	gone, err := c.writeSidecarIfCover(path, toneSidecar{Version: toneAlgoVersion})
	require.NoError(t, err)
	assert.True(t, gone)
	assert.NoFileExists(t, path+toneSidecarSuffix)

	calls := countSampler(t)
	c.computeTone(path) // 封面已经不在：静默返回
	assert.EqualValues(t, 1, calls.Load())
	_, known := c.memoTone(path)
	assert.False(t, known, "封面不在时不记失败，免得再下载回来后十分钟内没有颜色")
}

// Run：下载落盘超限 → 后台淘汰；ctx 结束时最后刷一次索引再返回。
func TestRunEvictsInBackgroundAndFlushesOnExit(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), 1000)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()
	for i := 0; i < 15; i++ {
		putCover(t, c, clk, coverURL(i), 100)
	}
	require.Eventually(t, func() bool { return c.Usage().Bytes <= 1000 }, 5*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run 没有在 ctx 结束后返回")
	}
	used, ok := c.readIndexFile()
	require.True(t, ok)
	assert.Len(t, used, c.Usage().Count, "退出前把索引刷到盘上")
}

// 并发取图 / 落盘 / 取色记账 / 淘汰 / 清空（-race 下跑）：之后索引与目录必须对得上。
func TestConcurrentOpenCommitEvictClear(t *testing.T) {
	c, clk := newCappedCache(t, t.TempDir(), 2000)
	c.SetPinned(func() []string { return []string{coverURL(1), coverURL(2)} })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	var opened atomic.Int32
	var commitErr atomic.Value
	put := func(url string, size int) {
		if err := commitCover(c, clk, url, size); err != nil {
			commitErr.Store(err)
		}
	}
	worker := func(fn func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				fn(i)
			}
		}()
	}
	worker(func(i int) { put(coverURL(i%60), 50+i%7) })
	worker(func(i int) { put(coverURL(60+i%40), 30) })
	worker(func(i int) {
		f, err := c.openTracked(c.Path(coverURL(i % 60)))
		if err == nil {
			opened.Add(1)
			buf := make([]byte, 8)
			_, _ = f.Read(buf)
			_ = f.Close()
		}
	})
	worker(func(i int) { c.PosterTones(coverURL(i % 100)) })
	worker(func(int) { c.evict(ctx) })
	worker(func(i int) {
		if i%50 == 0 {
			c.Clear()
		}
		c.Usage()
	})
	worker(func(i int) {
		if i%20 == 0 {
			c.flushIndex(false)
		}
	})
	wg.Wait()
	c.toneWG.Wait()

	assert.Nil(t, commitErr.Load())
	assert.Positive(t, opened.Load())
	assertIndexMatchesDir(t, c)
}

// assertIndexMatchesDir：索引里的每张封面都在盘上、大小对得上，总量等于各条之和；盘上没有不在索引里的封面，
// 没有正在使用的计数残留，每个取色边车都有主人。
func assertIndexMatchesDir(t *testing.T, c *Cache) {
	t.Helper()
	c.idx.mu.Lock()
	defer c.idx.mu.Unlock()
	var sum int64
	for name, e := range c.idx.entries {
		st, err := os.Stat(filepath.Join(c.dir, name))
		require.NoError(t, err, "索引里的 %s 不在盘上", name)
		assert.Equal(t, st.Size(), e.size, name)
		sum += e.size
	}
	assert.Equal(t, sum, c.idx.total)
	assert.Empty(t, c.idx.inUse)
	for _, name := range dirNames(t, c.dir) {
		switch {
		case isCoverName(name):
			_, indexed := c.idx.entries[name]
			assert.True(t, indexed, "盘上的 %s 不在索引里", name)
		case strings.HasSuffix(name, toneSidecarSuffix):
			assert.FileExists(t, filepath.Join(c.dir, strings.TrimSuffix(name, toneSidecarSuffix)), "边车 %s 没有主人", name)
		}
	}
}

// Open 从下载到交出文件全程护住它：Close 之前清空也删不掉。
func TestOpenProtectsFileUntilClose(t *testing.T) {
	srv := newImageServer(t)
	c := newTestCache(t)
	c.hc.CheckRedirect = nil
	f, err := c.openUnvalidated(context.Background(), srv.URL)
	require.NoError(t, err)
	_, skipped := c.Clear()
	assert.Equal(t, 1, skipped)
	buf := make([]byte, len(pngBytes))
	_, err = f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, pngBytes, buf)
	require.NoError(t, f.Close())
	u, skipped := c.Clear()
	assert.Zero(t, skipped)
	assert.Zero(t, u.Count)
}
