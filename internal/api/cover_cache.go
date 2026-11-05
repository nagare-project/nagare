package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/nagare-project/nagare/internal/httpserver"
)

// 封面缓存（internal/artcache）在设置页的那一部分：占用、清空，以及告诉它哪些封面「在用」、要最后才淘汰。
//
// 封面缓存比 Handler 晚建（要等能力段），所以和海报取色一样在运行中注入（SetCoverCache）；
// 注入之前设置载荷里的 coverCache 是 null，清空接口答 503。

// CoverCache 是设置页要的封面缓存能力；internal/artcache 的 *Cache 实现它。
type CoverCache interface {
	Usage() artcache.Usage
	Clear() (artcache.Usage, int)
}

// 接口签名一漂移，设置页就悄悄没了这一栏 —— 让编译器来守。
var _ CoverCache = (*artcache.Cache)(nil)

type coverCacheBox struct{ c CoverCache }

// coverCacheSlot 是可以在运行中注入的封面缓存。
type coverCacheSlot struct{ v atomic.Value }

func (s *coverCacheSlot) set(c CoverCache) { s.v.Store(coverCacheBox{c}) }

func (s *coverCacheSlot) get() CoverCache {
	box, _ := s.v.Load().(coverCacheBox)
	return box.c
}

// SetCoverCache 接上封面缓存（设置页的占用与清空）。启动时调用一次。
func (h *Handler) SetCoverCache(c CoverCache) { h.coverCache.set(c) }

// coverCacheView 是设置载荷里的 coverCache：{bytes, count, maxBytes}；这次运行没有封面缓存时为 null。
func (h *Handler) coverCacheView() *artcache.Usage {
	c := h.coverCache.get()
	if c == nil {
		return nil
	}
	u := c.Usage()
	return &u
}

// coverCacheClearView 是清空接口的响应：清完之后的占用，外加这次没删掉的张数（正在显示、或删不掉）。
type coverCacheClearView struct {
	artcache.Usage
	Skipped int `json:"skipped"`
}

// clearCoverCache 是 POST /api/cover-cache/clear：清空封面缓存。封面会在下次显示时重新下载。
func (h *Handler) clearCoverCache(w http.ResponseWriter, _ *http.Request) {
	c := h.coverCache.get()
	if c == nil {
		httpserver.WriteError(w, http.StatusServiceUnavailable, "封面缓存不可用")
		return
	}
	u, skipped := c.Clear()
	httpserver.WriteJSON(w, http.StatusOK, coverCacheClearView{Usage: u, Skipped: skipped})
}

// PinnedArtSources 列出封面缓存里要最后才淘汰的封面原始地址：媒体库认出作品的封面（匹配结果与手动认定），
// 以及落盘目录里用到的图片（离线时发现页靠它们）。交给 artcache.Cache.SetPinned，只在后台淘汰时调用。
func (h *Handler) PinnedArtSources() []string {
	var out []string
	if h.deps.Store != nil {
		out = h.deps.Store.CoverURLs()
	}
	if d := h.catalog.cache.disk; d != nil {
		out = append(out, d.imageSources()...)
	}
	return out
}

// maxImageSourceLen 是认作图片地址的字符串长度上限：再长就不是正常的图床地址了。
const maxImageSourceLen = 2048

// imageSources 列出盘上各份目录里出现的图片原始地址（去重）。
//
// 不按字段取，而是收集 data 里所有 https 开头的字符串：上游结构加了新的图片字段也不用跟着改；
// 混进来的非图片地址换算成缓存文件名后对不上任何封面，不影响结果。逐个文件读盘，不在请求路径上用。
func (d *catalogDisk) imageSources() []string {
	d.mu.Lock()
	names := make([]string, 0, len(d.index))
	for name := range d.index {
		names = append(names, name)
	}
	d.mu.Unlock()

	seen := map[string]bool{}
	var out []string
	var readErr error
	for _, name := range names {
		data, err := readLimited(filepath.Join(d.dir, name), catalogDiskMaxEntry)
		if err != nil {
			// 不存在：读之前刚被淘汰或替换掉了，是常态
			if !errors.Is(err, fs.ErrNotExist) && readErr == nil {
				readErr = err
			}
			continue
		}
		var file catalogDiskFile
		if json.Unmarshal(data, &file) != nil || file.Version != catalogDiskVersion {
			continue // 坏文件由读路径（load）删掉并记日志，这里只是不算它
		}
		collectHTTPSStrings(file.Data, func(s string) {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		})
	}
	if readErr != nil {
		log.Printf("目录缓存：列出离线目录用到的图片时有文件读不了（这些图可能先被封面缓存淘汰）：%v", readErr)
	}
	return out
}

// readLimited 读一个文件，超过 limit 字节报错（不整个读进内存）。
func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("文件超过单条上限")
	}
	return data, nil
}

// collectHTTPSStrings 逐个词法单元扫一遍 JSON，把 https 开头的字符串交给 fn（不建整棵树）。
func collectHTTPSStrings(raw json.RawMessage, fn func(string)) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := dec.Token()
		if err != nil {
			return // io.EOF 是正常结束；格式错误的文件由读路径处理
		}
		if s, ok := tok.(string); ok && strings.HasPrefix(s, "https://") && len(s) <= maxImageSourceLen {
			fn(s)
		}
	}
}
