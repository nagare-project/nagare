package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/nagare-project/nagare/internal/store"
)

// ArtHandler 按 fileId 提供番剧封面。
//
// 挂在能力 URL 下（/art/<能力段>/<fileId>），能力段在转发前已被剥掉，
// 处理器只看得到 /<fileId>。另有两种键：remote/<键>（目录图片登记表）与
// assoc/<clusterKey>（用户手动认定的作品封面）。
//
// 客户端给的是 fileId，不是图片地址 —— 真实地址从本机 store 的匹配结果里查。
// 这条设计是有意的：它让「让 nagare 去请求任意 URL」这个入口在客户端侧根本不存在。
type ArtHandler struct {
	st     *store.Store
	cache  *artcache.Cache
	remote RemoteArtSource
}

// NewArtHandler 构造封面处理器。
func NewArtHandler(st *store.Store, cache *artcache.Cache, remote RemoteArtSource) *ArtHandler {
	return &ArtHandler{st: st, cache: cache, remote: remote}
}

func (h *ArtHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/")
	if fileID == "" {
		http.NotFound(w, r)
		return
	}
	var source string
	var ok bool
	switch {
	case strings.HasPrefix(fileID, "remote/"):
		if h.remote != nil {
			source, ok = h.remote.Source(strings.TrimPrefix(fileID, "remote/"))
		}
	case strings.HasPrefix(fileID, assocCoverPrefix):
		// 手动认定的作品封面：键是 clusterKey，地址从本机关联记录里查
		a, found := h.st.Association(strings.TrimPrefix(fileID, assocCoverPrefix))
		source, ok = a.CoverURL, found && a.Mode == store.AssociationManual
	default:
		b, found := h.st.Binding(fileID)
		source, ok = b.CoverURL, found
	}

	if !ok || source == "" {
		// 没匹配过、或匹配结果里没有图 —— 都是常态，界面走无图版式。
		http.NotFound(w, r)
		return
	}

	path, err := h.cache.Get(r.Context(), source)
	if err != nil {
		// 不记录 URL：请求 URL 与上游地址都不进日志（见 middleware.go 的约束），
		// 这里只说哪个 fileId 失败了。
		if !errors.Is(err, r.Context().Err()) {
			log.Printf("art: 取封面失败（%q）：%v", fileID, withoutURL(err))
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

// withoutURL 去掉 *url.Error 里带着的上游图片地址：日志里不记任何 URL。
func withoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}
