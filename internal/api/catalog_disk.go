package api

import (
	"crypto/sha256"
	"encoding/hex"
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
	"time"
)

// 目录的磁盘层：把公开目录（发现页各板块、季度、放送、作品详情）的上游数据落到
// <配置目录>/cache/catalog/，重启之后、上游连不上时，发现页仍能先显示上一份。
//
// 落盘的是上游结构（CatalogMedia、ScheduleData 等，投影之前）的 JSON，不是投影后的视图：
// 视图里的图片地址带着本次进程的能力段，重启就失效；上游结构里是原始图片地址，
// 读回来重新投影时会照常登记进图片登记表（键 = 地址的 SHA-256，跨进程不变），
// 封面图本身已在 internal/artcache 的盘上，于是离线时 /art/<能力段>/remote/<键> 照样能出图。
//
// 只收白名单里的键（catalogDiskKey）。收藏、搜索以及任何与账号有关的数据一律不落盘：
// 断网时换了账号，盘上的旧收藏会被当成新账号的显示出来。

const (
	// catalogDiskVersion 是盘上文件的格式版本。改了文件结构或上游结构的含义就加一，
	// 旧版本的文件读到时直接丢弃、回到网络。
	catalogDiskVersion = 1
	// catalogDiskMaxBytes 是整个目录磁盘层的总上限，超过就按最近使用淘汰。
	catalogDiskMaxBytes = 50 << 20
	// catalogDiskMaxEntry 是单个文件的上限：季度一页 200 部约 1MB，8MB 已经很宽松，
	// 超过的不写，读到超过的（被改过的文件）直接丢弃，不整个读进内存。
	catalogDiskMaxEntry = 8 << 20

	catalogDiskExt        = ".json"
	catalogDiskTempPrefix = ".tmp-"
)

// catalogDiskPrefixes 是允许落盘的缓存键前缀（显式白名单，见 discover.go / seasonal.go 的键）。
// 不在这里的键一律只留内存：search:（搜索词本身就是用户输入）、收藏（另一个缓存实例，键是账号代次）。
var catalogDiskPrefixes = []string{
	"trending:", "thisSeason:", "pastSeason:", "nextSeason:", "gems:", "yearly:",
	"seasonal:", "detail:",
}

// catalogDiskKey 判断一个缓存键能不能落盘。
func catalogDiskKey(key string) bool {
	if key == "schedule" {
		return true
	}
	for _, p := range catalogDiskPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// catalogDiskFile 是盘上一个文件的内容。Key 一并存下，读回时核对，防止文件被挪错位置。
type catalogDiskFile struct {
	Version   int             `json:"version"`
	Key       string          `json:"key"`
	FetchedAt time.Time       `json:"fetchedAt"`
	Data      json.RawMessage `json:"data"`
}

type catalogDiskEntry struct {
	size int64
	used time.Time
}

// catalogDisk 是有总量上限的 LRU 文件缓存。最近使用时间同时写进文件的 mtime，
// 重启之后淘汰顺序不丢。并发安全：同键写入由上层的 singleflight 合并，不同键互不影响。
type catalogDisk struct {
	dir      string
	maxBytes int64
	now      func() time.Time

	mu    sync.Mutex
	index map[string]catalogDiskEntry // 文件名 → 大小与最近使用时间
	total int64
}

// newCatalogDisk 建目录（0700）并扫描已有文件；上次没写完的临时文件顺手删掉。
func newCatalogDisk(dir string, maxBytes int64) (*catalogDisk, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建目录缓存目录: %w", err)
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取目录缓存目录: %w", err)
	}
	d := &catalogDisk{dir: dir, maxBytes: maxBytes, now: time.Now, index: map[string]catalogDiskEntry{}}
	for _, it := range items {
		name := it.Name()
		if strings.HasPrefix(name, catalogDiskTempPrefix) {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.Printf("目录缓存：删除残留临时文件失败：%v", err)
			}
			continue
		}
		if !it.Type().IsRegular() || !strings.HasSuffix(name, catalogDiskExt) {
			continue
		}
		info, err := it.Info()
		if err != nil {
			continue // 扫描与删除赛跑：文件刚被删掉
		}
		d.index[name] = catalogDiskEntry{size: info.Size(), used: info.ModTime()}
		d.total += info.Size()
	}
	d.mu.Lock()
	d.evictLocked("")
	d.mu.Unlock()
	return d, nil
}

func catalogDiskName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:]) + catalogDiskExt
}

// load 读回某个键的上游数据。缺文件是常态（返回 false，不记日志）；
// 损坏、版本不符、键不符的文件删掉并记一行，调用方照常走网络。
func (d *catalogDisk) load(key string) (json.RawMessage, time.Time, bool) {
	name := catalogDiskName(key)
	path := filepath.Join(d.dir, name)
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("目录缓存：读取 %s 失败：%v", key, err)
		}
		d.forget(name)
		return nil, time.Time{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, catalogDiskMaxEntry+1))
	_ = f.Close() // 只读句柄，关闭失败不影响已读到的内容
	if err != nil {
		log.Printf("目录缓存：读取 %s 失败：%v", key, err)
		return nil, time.Time{}, false
	}
	if len(data) > catalogDiskMaxEntry {
		d.discard(key, "文件超过单条上限")
		return nil, time.Time{}, false
	}
	var file catalogDiskFile
	switch {
	case json.Unmarshal(data, &file) != nil:
		d.discard(key, "文件损坏")
		return nil, time.Time{}, false
	case file.Version != catalogDiskVersion:
		d.discard(key, fmt.Sprintf("格式版本 %d 与当前 %d 不符", file.Version, catalogDiskVersion))
		return nil, time.Time{}, false
	case file.Key != key || file.FetchedAt.IsZero() || len(file.Data) == 0:
		d.discard(key, "内容与键不符")
		return nil, time.Time{}, false
	}
	d.touch(name, path)
	return file.Data, file.FetchedAt, true
}

// store 原子落盘（临时文件 + 改名，0600）。写不进去只记日志：磁盘层是锦上添花，
// 不能让一次写盘失败把已经从上游拿到的数据变成错误。
func (d *catalogDisk) store(key string, value any, fetchedAt time.Time) {
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("目录缓存：编码 %s 失败：%v", key, err)
		return
	}
	buf, err := json.Marshal(catalogDiskFile{Version: catalogDiskVersion, Key: key, FetchedAt: fetchedAt.UTC(), Data: data})
	if err != nil {
		log.Printf("目录缓存：编码 %s 失败：%v", key, err)
		return
	}
	if len(buf) > catalogDiskMaxEntry || int64(len(buf)) > d.maxBytes {
		log.Printf("目录缓存：%s 有 %d 字节，超过上限，不落盘", key, len(buf))
		return
	}
	tmpName, err := writeTemp(d.dir, buf)
	if err != nil {
		log.Printf("目录缓存：写入 %s 失败：%v", key, err)
		return
	}
	defer os.Remove(tmpName) // 成功路径上已经改名走了，这里只清理失败的
	name := catalogDiskName(key)
	path := filepath.Join(d.dir, name)
	// 改名与索引更新在同一把锁里：否则另一个键的淘汰可能在这两步之间删掉刚改名到位的新文件，
	// 索引就记着一个不存在的文件（总量也跟着虚高）。
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := os.Rename(tmpName, path); err != nil {
		log.Printf("目录缓存：写入 %s 失败：%v", key, err)
		return
	}
	now := d.now()
	// mtime 与内存索引用同一只钟：重启后按 mtime 重建的淘汰顺序才与运行时一致
	if err := os.Chtimes(path, now, now); err != nil {
		log.Printf("目录缓存：更新使用时间失败（只影响淘汰顺序）：%v", err)
	}
	d.total += int64(len(buf)) - d.index[name].size
	d.index[name] = catalogDiskEntry{size: int64(len(buf)), used: now}
	d.evictLocked(name)
}

// discard 删掉一个读不了的文件；读的人照常走网络。
func (d *catalogDisk) discard(key, reason string) {
	name := catalogDiskName(key)
	log.Printf("目录缓存：丢弃 %s（%s），改从网络读取", key, reason)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := os.Remove(filepath.Join(d.dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("目录缓存：删除 %s 失败：%v", key, err)
		return // 删不掉就留在索引里，总量仍按它算
	}
	d.forgetLocked(name)
}

func (d *catalogDisk) forget(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.forgetLocked(name)
}

func (d *catalogDisk) forgetLocked(name string) {
	if e, ok := d.index[name]; ok {
		d.total -= e.size
		delete(d.index, name)
	}
}

// touch 记下最近使用：内存索引管本次运行的淘汰，mtime 管重启之后的。
func (d *catalogDisk) touch(name, path string) {
	now := d.now()
	d.mu.Lock()
	if e, ok := d.index[name]; ok {
		e.used = now
		d.index[name] = e
	}
	d.mu.Unlock()
	if err := os.Chtimes(path, now, now); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("目录缓存：更新使用时间失败（只影响淘汰顺序）：%v", err)
	}
}

// evictLocked 超过总上限时从最久没用的开始删，keep（刚写入的那个）不删。调用方持有 d.mu。
//
// 删不掉的文件（Windows 上正被读取的文件不让删）留在索引里、总量照算，这一轮跳过它去删下一个：
// 不然它成了不受管的孤儿文件，总量也会少算。
func (d *catalogDisk) evictLocked(keep string) {
	skip := map[string]bool{}
	for d.total > d.maxBytes {
		oldest := ""
		for name, e := range d.index {
			if name != keep && !skip[name] && (oldest == "" || e.used.Before(d.index[oldest].used)) {
				oldest = name
			}
		}
		if oldest == "" {
			return
		}
		if err := os.Remove(filepath.Join(d.dir, oldest)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Printf("目录缓存：淘汰旧文件失败（下次再试）：%v", err)
			skip[oldest] = true
			continue
		}
		d.total -= d.index[oldest].size
		delete(d.index, oldest)
	}
}

// writeTemp 把内容写进同目录下的临时文件（0600）并关闭，返回它的路径；改名由调用方在锁内完成，
// 读的人要么看到旧的完整文件，要么看到新的。
func writeTemp(dir string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil { // 用户可能在运行中清掉了缓存目录
		return "", err
	}
	tmp, err := os.CreateTemp(dir, catalogDiskTempPrefix+"*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return tmpName, nil
}
