package artcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 封面缓存的使用索引：每张封面最近一次被用到的时间，淘汰（evict.go）按它从最久没用的开始删。
//
// 为什么自己记，不看文件系统的 atime：atime 经常是关着的（noatime / relatime），macOS 与 Windows
// 上也不可靠。也不像目录磁盘层那样把使用时间写进 mtime：那样每出一张图就多一次写元数据的系统调用，
// 而 mtime 还是 /art 响应里 Last-Modified 的来源。
//
// 内存里一份（并发安全），盘上 index.json 一份（临时文件 + 改名，读的人永远看不到半个文件）。
// 盘上那份只是「最近使用」的提示，不是真相：启动时以目录扫描为准 —— 索引里有、盘上没有的条目丢掉，
// 盘上有、索引里没有的按 mtime 补上；索引读不了或格式不对就整个按目录重建。所以进程崩溃最多丢掉
// 最后一个刷盘间隔里的使用顺序，不会丢封面，也不会让索引与目录对不上。

const (
	indexFileName = "index.json"
	// indexVersion 是 index.json 的格式版本；改了结构就加一，旧文件按目录重建。
	indexVersion = 1
	// maxIndexFileBytes：索引文件读入的上限。每条约 90 字节，文件数上限 maxCacheEntries，
	// 正常远小于它；超过就当作损坏，按目录重建，不整个读进内存。
	maxIndexFileBytes = 8 << 20

	coverExt          = ".img"
	dlTempPrefix      = ".dl-"
	toneTempPrefix    = ".tone-"
	indexTempPrefix   = ".index-"
	coverNameHexChars = 64 // sha256 的十六进制
)

// indexFile 是 index.json 的内容：文件名 → 最近使用时间（Unix 纳秒）。
type indexFile struct {
	Version int              `json:"version"`
	Used    map[string]int64 `json:"used"`
}

// coverEntry 是内存索引里的一张封面。
type coverEntry struct {
	size int64
	used int64 // Unix 纳秒
}

// coverIndex 管内存索引、正在被读的文件、以及「删文件 / 改名进来 / 写边车」这几件必须互斥的事。
//
// mu 同时充当文件锁：落盘新封面（改名 + 记账）、淘汰（检查 + 删文件 + 记账）、写取色边车（确认封面还在 + 写）
// 都在它里面做，所以淘汰不会删掉一个刚改名到位、还没记进索引的封面，也不会在边车写到一半时把封面删掉、
// 留下一个没有主人的边车。持有 mu 时不去拿 Cache 的其他锁（inflight 的 mu、toneMu）。
type coverIndex struct {
	mu      sync.Mutex
	entries map[string]coverEntry // 文件名（<sha256>.img）→ 大小与最近使用
	total   int64
	inUse   map[string]int // 正在被 /art 读的文件（Open 到 Close 之间），淘汰与清空都跳过
	dirty   bool           // 内存里有还没刷到 index.json 的变化

	// flushMu 让刷盘串行：两次刷盘交错时，旧快照可能在新快照之后改名到位。
	flushMu sync.Mutex
}

// isCoverName 判断一个文件名是不是本缓存的封面：<64 位小写十六进制>.img。
func isCoverName(name string) bool {
	if len(name) != coverNameHexChars+len(coverExt) || !strings.HasSuffix(name, coverExt) {
		return false
	}
	for _, r := range name[:coverNameHexChars] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// coverName 返回 path 在本缓存里的文件名；不是本缓存目录下的封面时 ok = false
// （测试会把 download 的目标指到别的目录，那些文件不进索引）。
func (c *Cache) coverName(path string) (string, bool) {
	if filepath.Dir(path) != c.dir {
		return "", false
	}
	name := filepath.Base(path)
	return name, isCoverName(name)
}

// loadIndex 启动时读 index.json 并与目录对账（见文件头注释）。只在 New 里调用，此时没有并发。
func (c *Cache) loadIndex() error {
	used, ok := c.readIndexFile()
	items, err := os.ReadDir(c.dir)
	if err != nil {
		return fmt.Errorf("读取封面缓存目录: %w", err)
	}
	present := map[string]bool{}
	var sidecars []string
	for _, it := range items {
		name := it.Name()
		switch {
		case strings.HasPrefix(name, dlTempPrefix), strings.HasPrefix(name, toneTempPrefix),
			strings.HasPrefix(name, indexTempPrefix):
			// 上次进程没写完的临时文件（单实例：此刻不会有别人在写）
			c.removeQuietly(name, "残留临时文件")
		case strings.HasSuffix(name, toneSidecarSuffix):
			sidecars = append(sidecars, name)
		case isCoverName(name) && it.Type().IsRegular():
			info, err := it.Info()
			if err != nil {
				continue // 扫描与删除赛跑：文件刚被删掉
			}
			if info.Size() == 0 {
				c.removeQuietly(name, "空文件")
				continue
			}
			t, known := used[name]
			if !known {
				t = info.ModTime().UnixNano()
				c.idx.dirty = true
			}
			c.idx.entries[name] = coverEntry{size: info.Size(), used: t}
			c.idx.total += info.Size()
			present[name] = true
		}
	}
	// 没有主人的边车（封面被删、边车留下）：删掉，免得它们一直占着目录
	for _, sc := range sidecars {
		if !present[strings.TrimSuffix(sc, toneSidecarSuffix)] {
			c.removeQuietly(sc, "没有对应封面的取色结果")
		}
	}
	if !ok || len(used) != len(present) {
		c.idx.dirty = true
	}
	return nil
}

// readIndexFile 读 index.json。ok = false：没有（首次运行，不记日志）或读不了 / 损坏 / 版本不符（记一行，按目录重建）。
func (c *Cache) readIndexFile() (map[string]int64, bool) {
	f, err := os.Open(filepath.Join(c.dir, indexFileName))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("artcache: 读取封面缓存索引失败，按目录重建：%v", err)
		}
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxIndexFileBytes+1))
	_ = f.Close() // 只读句柄，关闭失败不影响已读到的内容
	if err != nil {
		log.Printf("artcache: 读取封面缓存索引失败，按目录重建：%v", err)
		return nil, false
	}
	var idx indexFile
	switch {
	case len(raw) > maxIndexFileBytes:
		log.Print("artcache: 封面缓存索引超过上限，按目录重建")
		return nil, false
	case json.Unmarshal(raw, &idx) != nil:
		log.Print("artcache: 封面缓存索引损坏，按目录重建")
		return nil, false
	case idx.Version != indexVersion:
		log.Printf("artcache: 封面缓存索引版本 %d 与当前 %d 不符，按目录重建", idx.Version, indexVersion)
		return nil, false
	}
	return idx.Used, true
}

// removeQuietly 删掉缓存目录里一个不再需要的文件；删不掉只记日志（下次启动再试）。
func (c *Cache) removeQuietly(name, what string) {
	if err := os.Remove(filepath.Join(c.dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("artcache: 删除%s失败（%s）：%v", what, name, err)
	}
}

// flushIndex 把内存索引刷到 index.json。force = false 时只在有变化时写。
// 写不进去只记日志：索引只影响淘汰顺序，下次启动按目录重建。
func (c *Cache) flushIndex(force bool) {
	c.idx.flushMu.Lock()
	defer c.idx.flushMu.Unlock()

	c.idx.mu.Lock()
	if !force && !c.idx.dirty {
		c.idx.mu.Unlock()
		return
	}
	snap := indexFile{Version: indexVersion, Used: make(map[string]int64, len(c.idx.entries))}
	for name, e := range c.idx.entries {
		snap.Used[name] = e.used
	}
	c.idx.dirty = false
	c.idx.mu.Unlock()

	if err := writeIndexFile(c.dir, snap); err != nil {
		log.Printf("artcache: 写入封面缓存索引失败（只影响淘汰顺序）：%v", err)
		c.idx.mu.Lock()
		c.idx.dirty = true // 下一轮再试
		c.idx.mu.Unlock()
	}
}

// writeIndexFile 原子写 index.json（临时文件 + 改名，0600）。
func writeIndexFile(dir string, snap indexFile) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, indexTempPrefix+"*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 成功路径上已经改名走了
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, indexFileName))
}

// commitFile 把下载好的临时文件改名到位，并在同一把锁里记进索引（见 coverIndex 的注释）。
func (c *Cache) commitFile(tmpName, dest string, size int64) error {
	c.idx.mu.Lock()
	if err := os.Rename(tmpName, dest); err != nil {
		c.idx.mu.Unlock()
		return err
	}
	over := false
	if name, ok := c.coverName(dest); ok {
		c.recordLocked(name, size)
		over = c.overLimitLocked()
	}
	c.idx.mu.Unlock()
	if over {
		c.requestEvict()
	}
	return nil
}

// recordLocked 记下（或替换）一张封面，最近使用 = 现在。调用方持有 idx.mu。
func (c *Cache) recordLocked(name string, size int64) {
	old := c.idx.entries[name]
	c.idx.total += size - old.size
	c.idx.entries[name] = coverEntry{size: size, used: c.now().UnixNano()}
	c.idx.dirty = true
}

// touch 记下一次使用。只更新已经在索引里的封面：不在的（还没下载）什么都不做，不碰磁盘。
func (c *Cache) touch(path string) {
	name, ok := c.coverName(path)
	if !ok {
		return
	}
	c.idx.mu.Lock()
	defer c.idx.mu.Unlock()
	if e, found := c.idx.entries[name]; found {
		e.used = c.now().UnixNano()
		c.idx.entries[name] = e
		c.idx.dirty = true
	}
}

// coverFile 是 Open 交出去的文件：Close 之前它不会被淘汰或清空。
type coverFile struct {
	*os.File
	c       *Cache
	name    string
	release sync.Once
}

// Close 关文件并解除「正在使用」。
func (f *coverFile) Close() error {
	err := f.File.Close()
	f.release.Do(func() {
		f.c.idx.mu.Lock()
		defer f.c.idx.mu.Unlock()
		if f.c.idx.inUse[f.name] <= 1 {
			delete(f.c.idx.inUse, f.name)
		} else {
			f.c.idx.inUse[f.name]--
		}
	})
	return err
}

// openTracked 在锁里打开封面并登记为正在使用，同时记一次使用。
// 不在索引里的封面（别处直接写进目录的）顺手补进索引。
func (c *Cache) openTracked(path string) (*coverFile, error) {
	name, ok := c.coverName(path)
	if !ok {
		return nil, fmt.Errorf("不是封面缓存里的文件：%s", filepath.Base(path))
	}
	c.idx.mu.Lock()
	defer c.idx.mu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if _, found := c.idx.entries[name]; found {
		e := c.idx.entries[name]
		e.used = c.now().UnixNano()
		c.idx.entries[name] = e
		c.idx.dirty = true
	} else if st, err := f.Stat(); err == nil && st.Size() > 0 {
		c.recordLocked(name, st.Size())
	}
	c.idx.inUse[name]++
	return &coverFile{File: f, c: c, name: name}, nil
}

// writeSidecarIfCover 写取色边车，但只在封面还在时写：淘汰可能已经删掉了正在取色的封面，
// 这时再写就留下一个没有主人的边车（gone = true，什么都不写）。在索引锁里做，与淘汰互斥。
func (c *Cache) writeSidecarIfCover(coverPath string, sc toneSidecar) (gone bool, err error) {
	c.idx.mu.Lock()
	defer c.idx.mu.Unlock()
	if _, err := os.Stat(coverPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return false, writeToneSidecar(coverPath, sc)
}
