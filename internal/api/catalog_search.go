package api

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nagare-project/nagare/internal/animego"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/httpserver"
)

// CatalogSearcher 是目录的关键词搜索（animego /api/anime/search）。单独成接口而不并进
// CatalogReader：其余目录读取都在 animego 的限速豁免区，只有它不在，而且会打到 AniList，
// 调用方式（只在提交时调、必须缓存、要节流）和别的读取不一样。
type CatalogSearcher interface {
	Search(context.Context, string) ([]animego.CatalogMedia, error)
}

// maxSearchRunes 是关键词长度上限（按字计，中文一个字算一个）。
const maxSearchRunes = 64

// searchCacheTTL 与 animego 自己的搜索缓存同为十分钟：同一个词再搜不再出站。
const searchCacheTTL = 10 * time.Minute

// searchMinInterval 是向 animego 发出两次搜索之间的最短间隔。animego 对这个端点按 IP
// 每秒放行约一次，超了回 429；在这里先挡住，用户看到的是「搜得太快了」而不是一个上游错误。
const searchMinInterval = time.Second

// CatalogSearchView 是 GET /api/catalog/search 的响应。
type CatalogSearchView struct {
	Query string         `json:"query"`
	Items []SummaryMedia `json:"items"`
}

// Search 按作品名搜目录。关键词先收拢空白（「芙莉莲  」与「芙莉莲」是同一次搜索、同一个缓存键）。
func (s *DiscoverService) Search(ctx context.Context, raw string) (CatalogSearchView, error) {
	q := strings.Join(strings.Fields(raw), " ")
	if q == "" {
		return CatalogSearchView{}, errs.New(errs.CategoryInput, "catalog.search", "请输入作品名", "")
	}
	if utf8.RuneCountInString(q) > maxSearchRunes {
		return CatalogSearchView{}, errs.New(errs.CategoryInput, "catalog.search", "作品名太长了", "换一个更短的关键词")
	}
	searcher, ok := s.source.(CatalogSearcher)
	if !ok {
		return CatalogSearchView{}, errs.New(errs.CategoryInternal, "catalog.search", "这个版本不支持搜索作品", "")
	}
	rows, err := cachedRead(ctx, s.cache, "search:"+strings.ToLower(q), searchCacheTTL, func(ctx context.Context) ([]animego.CatalogMedia, error) {
		if !s.allowSearch() {
			return nil, errs.New(errs.CategoryUpstream, "catalog.search", "搜得太快了", "稍等一两秒再搜")
		}
		return searcher.Search(ctx, q)
	})
	if err != nil {
		return CatalogSearchView{}, err
	}
	return CatalogSearchView{Query: q, Items: s.summaries(rows)}, nil
}

// allowSearch 是全局节流：距上一次出站不足 searchMinInterval 就拒绝。命中缓存的搜索不经过这里。
func (s *DiscoverService) allowSearch() bool {
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	now := s.cache.now()
	if !s.lastSearch.IsZero() && now.Sub(s.lastSearch) < searchMinInterval {
		return false
	}
	s.lastSearch = now
	return true
}

func (h *Handler) catalogSearch(w http.ResponseWriter, r *http.Request) {
	if !h.requireCatalog(w) {
		return
	}
	v, err := h.catalog.Search(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, v)
}
