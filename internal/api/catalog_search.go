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

// searchMinInterval 是向 animego 发出两次搜索之间的最短间隔。animego 对这个端点按 IP 限速
// （令牌桶，每秒补 1 个、最多攒 60 个），没命中它自己的库时还会打到 AniList。
// 不到间隔就【等】到点再发，而不是拒绝：偶尔连搜两次根本不会被限，拒绝只会让人多点一次。
const searchMinInterval = time.Second

// maxSearchWait 是排队等出站的上限。同时堆了好几个不同关键词才会超过它，那时直接说「搜得太快了」。
const maxSearchWait = 3 * time.Second

// 真实客户端必须满足 CatalogSearcher：搜索是靠类型断言接上的，签名一漂移，
// 每次真实搜索都会变成「不支持搜索」而替身测试照样全绿 —— 让编译器来守。
var _ CatalogSearcher = (*animego.Client)(nil)

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
		if err := s.waitSearchTurn(ctx); err != nil {
			return nil, err
		}
		return searcher.Search(ctx, q)
	})
	if err != nil {
		return CatalogSearchView{}, err
	}
	return CatalogSearchView{Query: q, Items: s.summaries(rows)}, nil
}

// waitSearchTurn 是全局节流：给这次出站预约一个与上一次至少隔 searchMinInterval 的时刻，
// 不到点就等；要等的时间超过 maxSearchWait 就拒绝。命中缓存的搜索不经过这里。
func (s *DiscoverService) waitSearchTurn(ctx context.Context) error {
	s.searchMu.Lock()
	now := s.cache.now()
	slot := now
	if next := s.lastSearch.Add(searchMinInterval); !s.lastSearch.IsZero() && next.After(now) {
		slot = next
	}
	wait := slot.Sub(now)
	if wait > maxSearchWait {
		s.searchMu.Unlock()
		return errs.New(errs.CategoryUpstream, "catalog.search", "搜得太快了", "稍等几秒再搜")
	}
	s.lastSearch = slot
	s.searchMu.Unlock()
	return s.sleep(ctx, wait)
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
