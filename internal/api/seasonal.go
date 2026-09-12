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

// SeasonalView 是 GET /api/seasonal 的响应：某一季的全部作品（animego 一页 200 条）。
type SeasonalView struct {
	Season    string         `json:"season"`
	Year      int            `json:"year"`
	Items     []SummaryMedia `json:"items"`
	FetchedAt int64          `json:"fetchedAt"`
}

var seasonNames = []string{"WINTER", "SPRING", "SUMMER", "FALL"}

// Seasonal 读某一季的目录，与 Discover 的季度板块共用同一套缓存键，翻到当季时不重复请求。
// 放送快照有就顺带补 nextAiring / recentAiring；没有不阻塞。
func (s *DiscoverService) Seasonal(ctx context.Context, season string, year int) (SeasonalView, error) {
	season = strings.ToUpper(strings.TrimSpace(season))
	quarter := -1
	for i, name := range seasonNames {
		if name == season {
			quarter = i
		}
	}
	if quarter < 0 {
		return SeasonalView{}, errs.New(errs.CategoryInput, "catalog.seasonal", "季度必须是 WINTER / SPRING / SUMMER / FALL", "")
	}
	if year < 1990 || year > 2100 {
		return SeasonalView{}, errs.New(errs.CategoryInput, "catalog.seasonal", "年份超出范围", "")
	}
	key := fmt.Sprintf("seasonal:%s:%d", season, year)
	rows, err := cachedRead(ctx, s.cache, key, 10*time.Minute, func(ctx context.Context) ([]animego.CatalogMedia, error) {
		return s.source.Seasonal(ctx, season, year)
	})
	if err != nil {
		return SeasonalView{}, err
	}
	view := SeasonalView{Season: season, Year: year, Items: s.summaries(rows), FetchedAt: s.cache.now().Unix()}
	if at := s.cache.fetchedAt(key); !at.IsZero() {
		view.FetchedAt = at.Unix()
	}
	if value, ok := s.cache.get("schedule"); ok {
		now := s.cache.now().Unix()
		next := nextAirings(value.(animego.ScheduleData), now)
		aired := recentAirings(value.(animego.ScheduleData), now)
		for i := range view.Items {
			view.Items[i].NextAiring = next[view.Items[i].AnilistID]
			view.Items[i].RecentAiring = aired[view.Items[i].AnilistID]
		}
	}
	return view, nil
}

func (h *Handler) catalogSeasonal(w http.ResponseWriter, r *http.Request) {
	if !h.requireCatalog(w) {
		return
	}
	year, err := strconv.Atoi(r.URL.Query().Get("year"))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "年份必须是整数")
		return
	}
	v, err := h.catalog.Seasonal(r.Context(), r.URL.Query().Get("season"), year)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, v)
}
