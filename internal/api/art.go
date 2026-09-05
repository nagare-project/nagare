package api

import (
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/nagare-project/nagare/internal/store"
)

// ArtHandler 按 fileId 提供番剧封面。
//
// 挂在能力 URL 下（/art/<能力段>/<fileId>），能力段在转发前已被剥掉，
// 处理器只看得到 /<fileId>。
//
// 客户端给的是 fileId，不是图片地址 —— 真实地址从本机 store 的匹配结果里查。
// 这条设计是有意的：它让「让 nagare 去请求任意 URL」这个入口在客户端侧根本不存在。
type ArtHandler struct {
	st            *store.Store
	cache         *artcache.Cache
	catalogSource func(string) (string, bool)
}

// NewArtHandler 构造封面处理器。
func NewArtHandler(st *store.Store, cache *artcache.Cache, catalogSource ...func(string) (string, bool)) *ArtHandler {
	h := &ArtHandler{st: st, cache: cache}
	if len(catalogSource) > 0 {
		h.catalogSource = catalogSource[0]
	}
	return h
}

func (h *ArtHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/")
	if fileID == "" {
		http.NotFound(w, r)
		return
	}
	b, ok := h.st.Binding(fileID)
	if strings.HasPrefix(fileID, "catalog/") && h.catalogSource != nil {
		b.CoverURL, ok = h.catalogSource(strings.TrimPrefix(fileID, "catalog/"))
	}
	if !ok || b.CoverURL == "" {
		// 没匹配过、或匹配结果里没有图 —— 都是常态，界面走无图版式。
		http.NotFound(w, r)
		return
	}

	path, err := h.cache.Get(r.Context(), b.CoverURL)
	if err != nil {
		// 不记录 URL：请求 URL 与上游地址都不进日志（见 middleware.go 的约束），
		// 这里只说哪个 fileId 失败了。
		if !errors.Is(err, r.Context().Err()) {
			log.Printf("art: 取封面失败（fileId=%s）：%v", fileID, err)
		}
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 封面按 URL 的 sha256 落盘，内容与路径一一对应，可以长期缓存。
	// private：这是本机地址，不该被任何中间层缓存（虽然本来也没有中间层）。
	w.Header().Set("Cache-Control", "private, max-age=604800, immutable")
	// 图片是外部来源，禁止浏览器嗅探成别的类型。
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "cover", st.ModTime(), f)
}
