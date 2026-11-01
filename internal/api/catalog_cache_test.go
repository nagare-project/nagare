package api

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 没有磁盘层的缓存（收藏、集号空间、集信息）行为与以前一致：过期即不可用。
func TestReadCacheWithoutDiskKeepsOldExpiry(t *testing.T) {
	clock := &diskClock{at: diskT0}
	c := newReadCache()
	c.now = clock.now
	c.put("detail:7", 1, time.Minute)
	clock.set(diskT0.Add(2 * time.Minute))
	_, fresh := c.get("detail:7")
	_, peeked := c.peek("detail:7")
	_, found := c.lookup("detail:7")
	require.False(t, fresh)
	require.False(t, peeked, "没有磁盘层时 peek 不给过期的")
	require.False(t, found, "没有磁盘层时不做 stale-while-revalidate")

	d, err := newCatalogDisk(t.TempDir(), catalogDiskMaxBytes)
	require.NoError(t, err)
	c.disk = d
	_, peeked = c.peek("detail:7")
	r, found := c.lookup("detail:7")
	require.True(t, peeked)
	require.True(t, found)
	require.True(t, r.meta.stale)
	_, found = c.lookup("search:x")
	require.False(t, found)
}

// 淘汰时删不掉的文件留在索引里、总量照算，改删下一个最旧的。
func TestCatalogDiskEvictionKeepsAccountingWhenRemoveFails(t *testing.T) {
	dir := t.TempDir()
	clock := &diskClock{at: diskT0}
	d, err := newCatalogDisk(dir, catalogDiskMaxBytes)
	require.NoError(t, err)
	d.now = clock.now
	payload := map[string]string{"x": strings.Repeat("目", 300)}
	d.store("detail:1", payload, diskT0)
	size := d.total

	// 一个删不掉的「文件」：同名的非空目录，而且最旧
	stuck := catalogDiskName("detail:0")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, stuck, "x"), 0o700))
	d.mu.Lock()
	d.index[stuck] = catalogDiskEntry{size: size, used: diskT0.Add(-time.Hour)}
	d.total += size
	d.maxBytes = 2*size + size/2
	d.mu.Unlock()

	clock.set(diskT0.Add(time.Minute))
	d.store("detail:2", payload, diskT0)
	d.mu.Lock()
	defer d.mu.Unlock()
	require.Contains(t, d.index, stuck, "删不掉的仍在账上")
	require.NotContains(t, d.index, catalogDiskName("detail:1"), "改删下一个最旧的")
	require.Contains(t, d.index, catalogDiskName("detail:2"))
	require.Equal(t, 2*size, d.total)
}

// 压力：超过内存条数上限的键、上游时好时坏、时钟推进逼出后台刷新，并发读写同一批键；配合 -race 跑。
// 结束后索引与盘上文件逐一对得上，总量没有漂移，也不留临时文件。
func TestCatalogDiskConcurrentReadsStayConsistent(t *testing.T) {
	dir := t.TempDir()
	up := &diskStub{title: "作品"}
	s, clock := diskService(t, dir, up, diskT0)
	s.cache.disk.maxBytes = 16 << 10 // 约五十份，逼出淘汰
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 60 {
				id := (g*37+i*11)%(readCacheMaxEntries+80) + 1
				if i%15 == 0 {
					up.mu.Lock()
					up.fail = !up.fail
					up.mu.Unlock()
				}
				if g == 0 && i%20 == 0 {
					clock.set(clock.now().Add(11 * time.Minute))
				}
				_, _ = s.Detail(context.Background(), id) // 断网又没有副本时报错是预期内的
			}
		}()
	}
	wg.Wait()
	s.cache.waitBackground()

	d := s.cache.disk
	d.mu.Lock()
	defer d.mu.Unlock()
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	var sum int64
	onDisk := map[string]int64{}
	for _, f := range files {
		require.False(t, strings.HasPrefix(f.Name(), catalogDiskTempPrefix), "不留临时文件")
		info, err := f.Info()
		require.NoError(t, err)
		onDisk[f.Name()] = info.Size()
		sum += info.Size()
	}
	require.NotEmpty(t, onDisk)
	var orphans []string // 盘上有、账上没有：不受管，总量少算、永远淘汰不到
	for name := range onDisk {
		if _, ok := d.index[name]; !ok {
			orphans = append(orphans, name)
		}
	}
	require.Empty(t, orphans, "盘上的文件都要在索引里")
	require.Len(t, d.index, len(onDisk))
	for name, e := range d.index {
		size, ok := onDisk[name]
		require.True(t, ok, "索引里的文件都在盘上")
		require.Equal(t, size, e.size)
	}
	require.Equal(t, sum, d.total)
	require.LessOrEqual(t, d.total, d.maxBytes)
}

// 读的时候文件不在、打开失败之后到划账之前，同键的另一次写入（后台刷新、另一个页面的读取）把新文件
// 落了盘：新文件要留在账上。以前照「刚才不在」直接划掉，盘上的新文件成了孤儿 —— 总量少算、
// 永远淘汰不到，上面的压力用例约 1.5% 的概率撞上（索引比盘上少一个文件）。
func TestCatalogDiskMissingReadKeepsConcurrentWrite(t *testing.T) {
	dir := t.TempDir()
	d, err := newCatalogDisk(dir, catalogDiskMaxBytes)
	require.NoError(t, err)
	const key = "detail:7"
	name := catalogDiskName(key)
	path := filepath.Join(dir, name)
	raced := false
	d.open = func(p string) (*os.File, error) {
		f, err := os.Open(p)
		if errors.Is(err, fs.ErrNotExist) {
			raced = true
			d.store(key, map[string]string{"title": "新"}, diskT0)
		}
		return f, err
	}

	_, _, ok := d.load(key)
	require.False(t, ok, "这一次打开时盘上还没有")
	require.True(t, raced, "要在打开失败之后才写入")
	info, err := os.Stat(path)
	require.NoError(t, err, "同键写入已经落盘")
	d.mu.Lock()
	entry, indexed := d.index[name]
	total := d.total
	d.mu.Unlock()
	require.True(t, indexed, "落了盘的文件要留在账上")
	require.Equal(t, info.Size(), entry.size)
	require.Equal(t, info.Size(), total)

	// 真的不在了（缓存目录被清过）：照样从账上划掉
	d.open = os.Open
	require.NoError(t, os.Remove(path))
	_, _, ok = d.load(key)
	require.False(t, ok)
	d.mu.Lock()
	defer d.mu.Unlock()
	require.NotContains(t, d.index, name)
	require.Zero(t, d.total)
}
