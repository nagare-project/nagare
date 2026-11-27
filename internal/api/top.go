package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/httpserver"
)

// TopReader 是排行榜（/api/anime/top 与 /api/anime/top/years）的窄接口，
// 独立于 CatalogReader，避免打断不测排行的桩实现；*animego.Client 天然满足它。
type TopReader interface {
	Top(context.Context, animego.TopQuery) ([]animego.CatalogMedia, error)
	TopYears(context.Context) ([]animego.TopYearCount, error)
}

// TopView 是 GET /api/top 的响应：全站总榜或按年份 / 形式 / 季度 / 类型筛选的 Top 100。
type TopView struct {
	Year      int            `json:"year,omitempty"`
	Format    string         `json:"format,omitempty"`
	Season    string         `json:"season,omitempty"`
	Genre     string         `json:"genre,omitempty"`
	Items     []SummaryMedia `json:"items"`
	FetchedAt int64          `json:"fetchedAt"`
	Freshness *Freshness     `json:"freshness,omitempty"`
}

// TopYearsView 是 GET /api/top/years 的响应：有排行的年份与各年作品数。
type TopYearsView struct {
	Years     []animego.TopYearCount `json:"years"`
	FetchedAt int64                  `json:"fetchedAt"`
	Freshness *Freshness             `json:"freshness,omitempty"`
}

var validTopFormats = map[string]bool{
	"TV": true, "TV_SHORT": true, "MOVIE": true, "ONA": true, "OVA": true, "SPECIAL": true,
}

var validTopSeasons = map[string]bool{
	"WINTER": true, "SPRING": true, "SUMMER": true, "FALL": true,
}

func normalizeTopQuery(q animego.TopQuery) (animego.TopQuery, error) {
	q.Format = strings.ToUpper(strings.TrimSpace(q.Format))
	q.Season = strings.ToUpper(strings.TrimSpace(q.Season))
	q.Genre = strings.TrimSpace(q.Genre)
	if q.Year != 0 && (q.Year < 1950 || q.Year > 2100) {
		return animego.TopQuery{}, errs.New(errs.CategoryInput, "catalog.top", "年份超出范围", "")
	}
	if q.Format != "" && !validTopFormats[q.Format] {
		return animego.TopQuery{}, errs.New(errs.CategoryInput, "catalog.top", "形式必须是 TV / TV_SHORT / MOVIE / ONA / OVA / SPECIAL", "")
	}
	if q.Season != "" {
		if !validTopSeasons[q.Season] {
			return animego.TopQuery{}, errs.New(errs.CategoryInput, "catalog.top", "季度必须是 WINTER / SPRING / SUMMER / FALL", "")
		}
		if q.Year == 0 {
			return animego.TopQuery{}, errs.New(errs.CategoryInput, "catalog.top", "按季度筛选排行需要同时指定年份", "")
		}
	}
	if len(q.Genre) > 40 {
		return animego.TopQuery{}, errs.New(errs.CategoryInput, "catalog.top", "类型参数过长", "")
	}
	return q, nil
}

func topCacheKey(q animego.TopQuery) string {
	return fmt.Sprintf("top:%d:%s:%s:%s", q.Year, q.Format, q.Season, q.Genre)
}

// Top 读排行榜（10 分钟内存缓存 + 磁盘层离线回退）。
func (s *DiscoverService) Top(ctx context.Context, rawQuery animego.TopQuery) (TopView, error) {
	reader, ok := s.source.(TopReader)
	if !ok {
		return TopView{}, errs.New(errs.CategoryUpstream, "catalog.top", "排行榜服务暂不可用", "")
	}
	q, err := normalizeTopQuery(rawQuery)
	if err != nil {
		return TopView{}, err
	}
	rows, meta, err := cachedReadMeta(ctx, s.cache, topCacheKey(q), 10*time.Minute, func(ctx context.Context) ([]animego.CatalogMedia, error) {
		return reader.Top(ctx, q)
	})
	if err != nil {
		return TopView{}, err
	}
	items := s.summaries(rows)
	for i := range items {
		if items[i].Rank <= 0 {
			items[i].Rank = i + 1
		}
	}
	return TopView{
		Year:      q.Year,
		Format:    q.Format,
		Season:    q.Season,
		Genre:     q.Genre,
		Items:     items,
		FetchedAt: meta.fetchedAt.Unix(),
		Freshness: freshnessOf(meta),
	}, nil
}

// TopYears 读有排行的年份列表（30 分钟缓存 + 磁盘层）。
func (s *DiscoverService) TopYears(ctx context.Context) (TopYearsView, error) {
	reader, ok := s.source.(TopReader)
	if !ok {
		return TopYearsView{}, errs.New(errs.CategoryUpstream, "catalog.top-years", "排行榜服务暂不可用", "")
	}
	years, meta, err := cachedReadMeta(ctx, s.cache, "top-years", 30*time.Minute, reader.TopYears)
	if err != nil {
		return TopYearsView{}, err
	}
	if years == nil {
		years = []animego.TopYearCount{}
	}
	return TopYearsView{
		Years:     years,
		FetchedAt: meta.fetchedAt.Unix(),
		Freshness: freshnessOf(meta),
	}, nil
}

func (h *Handler) catalogTop(w http.ResponseWriter, r *http.Request) {
	if !h.requireCatalog(w) {
		return
	}
	qs := r.URL.Query()
	var year int
	if raw := strings.TrimSpace(qs.Get("year")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			httpserver.WriteError(w, http.StatusBadRequest, "年份必须是整数")
			return
		}
		year = v
	}
	v, err := h.catalog.Top(r.Context(), animego.TopQuery{
		Year:   year,
		Format: qs.Get("format"),
		Season: qs.Get("season"),
		Genre:  qs.Get("genre"),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, v)
}

func (h *Handler) catalogTopYears(w http.ResponseWriter, r *http.Request) {
	if !h.requireCatalog(w) {
		return
	}
	v, err := h.catalog.TopYears(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, v)
}
